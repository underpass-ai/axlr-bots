use chrono::{DateTime, SecondsFormat, Utc};
use prost_types::{Duration, Timestamp};
use serde::de::DeserializeOwned;
use serde_json::{Map, Number, Value};

use crate::application::PanelError;

/// The small conversions every mapper shares. The panel's domain types read
/// the shape the operator API had in JSON (camelCase, RFC 3339 instants), so
/// a mapper builds that shape from a message and the domain type validates
/// it as it reads it: one place decides what a good value is.
pub(super) struct Wire;

impl Wire {
    /// An instant as RFC 3339 text, or null when the message has none.
    pub fn time(at: Option<&Timestamp>) -> Value {
        at.and_then(|t| DateTime::from_timestamp(t.seconds, u32::try_from(t.nanos).unwrap_or(0)))
            .map_or(Value::Null, |t| {
                Value::String(t.to_rfc3339_opts(SecondsFormat::AutoSi, true))
            })
    }

    /// A time the panel needs as a value, not as JSON.
    pub fn instant(at: Option<&Timestamp>) -> Option<DateTime<Utc>> {
        at.and_then(|t| DateTime::from_timestamp(t.seconds, u32::try_from(t.nanos).unwrap_or(0)))
    }

    pub fn timestamp(at: DateTime<Utc>) -> Timestamp {
        Timestamp {
            seconds: at.timestamp(),
            nanos: i32::try_from(at.timestamp_subsec_nanos()).unwrap_or(0),
        }
    }

    /// Text, or null when empty ("absent" in the contract).
    pub fn text(raw: &str) -> Value {
        if raw.is_empty() {
            Value::Null
        } else {
            Value::String(raw.to_string())
        }
    }

    /// A JSON document carried as bytes, verbatim; null when empty.
    pub fn document(bytes: &[u8]) -> Result<Value, PanelError> {
        if bytes.is_empty() {
            return Ok(Value::Null);
        }
        serde_json::from_slice(bytes).map_err(|e| PanelError::Unreadable(format!("a JSON field of the answer: {e}")))
    }

    /// A string map as a JSON object.
    pub fn strings<'a>(map: impl IntoIterator<Item = (&'a String, &'a String)>) -> Value {
        Value::Object(
            map.into_iter()
                .map(|(k, v)| (k.clone(), Value::String(v.clone())))
                .collect(),
        )
    }

    /// A `Struct` as a JSON object. Protobuf numbers are doubles: a whole
    /// one comes back as an integer, which the panel's readers expect.
    pub fn object(fields: &prost_types::Struct) -> Map<String, Value> {
        fields.fields.iter().map(|(k, v)| (k.clone(), Self::value(v))).collect()
    }

    fn value(value: &prost_types::Value) -> Value {
        use prost_types::value::Kind;
        match &value.kind {
            None | Some(Kind::NullValue(_)) => Value::Null,
            Some(Kind::BoolValue(b)) => Value::Bool(*b),
            Some(Kind::StringValue(s)) => Value::String(s.clone()),
            Some(Kind::NumberValue(n)) => Self::number(*n),
            Some(Kind::StructValue(s)) => Value::Object(Self::object(s)),
            Some(Kind::ListValue(l)) => Value::Array(l.values.iter().map(Self::value).collect()),
        }
    }

    fn number(n: f64) -> Value {
        // 2^53: the whole numbers a double holds exactly.
        const EXACT: f64 = 9_007_199_254_740_992.0;
        if n.fract() == 0.0 && n.abs() < EXACT {
            if n >= 0.0 {
                return Value::Number(Number::from(n as u64));
            }
            return Value::Number(Number::from(n as i64));
        }
        Number::from_f64(n).map_or(Value::Null, Value::Number)
    }

    /// A JSON value as a `Struct` value.
    pub fn struct_value(value: &Value) -> prost_types::Value {
        use prost_types::value::Kind;
        prost_types::Value {
            kind: Some(match value {
                Value::Null => Kind::NullValue(0),
                Value::Bool(b) => Kind::BoolValue(*b),
                Value::Number(n) => Kind::NumberValue(n.as_f64().unwrap_or(0.0)),
                Value::String(s) => Kind::StringValue(s.clone()),
                Value::Array(items) => Kind::ListValue(prost_types::ListValue {
                    values: items.iter().map(Self::struct_value).collect(),
                }),
                Value::Object(map) => Kind::StructValue(Self::structure(map)),
            }),
        }
    }

    pub fn structure(map: &Map<String, Value>) -> prost_types::Struct {
        prost_types::Struct {
            fields: map.iter().map(|(k, v)| (k.clone(), Self::struct_value(v))).collect(),
        }
    }

    /// Reads the shape a mapper built into a domain type.
    pub fn read<T: DeserializeOwned>(value: Value, what: &str) -> Result<T, PanelError> {
        serde_json::from_value(value).map_err(|e| PanelError::Unreadable(format!("the control plane's {what}: {e}")))
    }

    /// A duration as Go writes it (`1h30m5s`, `250ms`), the form bot files
    /// and the enrollment lifetime use. Zero is empty.
    pub fn duration_text(span: Option<&Duration>) -> String {
        let Some(span) = span else { return String::new() };
        let total_ms = span.seconds.saturating_mul(1000) + i64::from(span.nanos) / 1_000_000;
        if total_ms <= 0 {
            return String::new();
        }
        let (hours, rest) = (total_ms / 3_600_000, total_ms % 3_600_000);
        let (minutes, rest) = (rest / 60_000, rest % 60_000);
        let (seconds, millis) = (rest / 1000, rest % 1000);
        let mut text = String::new();
        if hours > 0 {
            text.push_str(&format!("{hours}h"));
        }
        if minutes > 0 {
            text.push_str(&format!("{minutes}m"));
        }
        if seconds > 0 {
            text.push_str(&format!("{seconds}s"));
        }
        if millis > 0 {
            text.push_str(&format!("{millis}ms"));
        }
        text
    }

    /// The whole seconds of a duration.
    pub fn seconds(span: Option<&Duration>) -> u32 {
        span.map_or(0, |d| u32::try_from(d.seconds.max(0)).unwrap_or(u32::MAX))
    }

    /// Reads a Go duration written with h, m, s and ms.
    pub fn parse_duration(raw: &str) -> Result<Duration, PanelError> {
        let invalid = || PanelError::Invalid(format!("{raw:?} is not a duration like 30m, 1h or 2h30m"));
        let mut millis: i64 = 0;
        let mut digits = String::new();
        let mut chars = raw.trim().chars().peekable();
        if chars.peek().is_none() {
            return Err(invalid());
        }
        while let Some(c) = chars.next() {
            if c.is_ascii_digit() {
                digits.push(c);
                continue;
            }
            let number: i64 = digits.parse().map_err(|_| invalid())?;
            digits.clear();
            let unit = match c {
                'h' => 3_600_000,
                'm' if chars.peek() == Some(&'s') => {
                    chars.next();
                    1
                }
                'm' => 60_000,
                's' => 1000,
                _ => return Err(invalid()),
            };
            millis = millis.saturating_add(number.saturating_mul(unit));
        }
        if !digits.is_empty() {
            return Err(invalid());
        }
        Ok(Duration {
            seconds: millis / 1000,
            nanos: i32::try_from((millis % 1000) * 1_000_000).unwrap_or(0),
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn whole_numbers_of_a_struct_are_integers() {
        let fields = prost_types::Struct {
            fields: [
                (
                    "dropped".to_string(),
                    prost_types::Value {
                        kind: Some(prost_types::value::Kind::NumberValue(3.0)),
                    },
                ),
                (
                    "ratio".to_string(),
                    prost_types::Value {
                        kind: Some(prost_types::value::Kind::NumberValue(0.5)),
                    },
                ),
                (
                    "debt".to_string(),
                    prost_types::Value {
                        kind: Some(prost_types::value::Kind::NumberValue(-2.0)),
                    },
                ),
            ]
            .into_iter()
            .collect(),
        };
        let object = Wire::object(&fields);
        assert_eq!(object["dropped"].as_u64(), Some(3));
        assert_eq!(object["ratio"].as_f64(), Some(0.5));
        assert_eq!(object["debt"].as_i64(), Some(-2));
        assert_eq!(Wire::object(&Wire::structure(&object)), object);
    }

    #[test]
    fn durations_go_both_ways_in_go_form() {
        for text in ["10m", "1h30m", "5s", "250ms", "2h"] {
            assert_eq!(Wire::duration_text(Some(&Wire::parse_duration(text).unwrap())), text);
        }
        assert_eq!(Wire::duration_text(None), "");
        assert_eq!(Wire::seconds(Some(&Wire::parse_duration("5m").unwrap())), 300);
        for bad in ["", "m", "10", "1x", "h1"] {
            assert!(Wire::parse_duration(bad).is_err(), "{bad}");
        }
    }

    #[test]
    fn instants_and_absent_values() {
        let at: DateTime<Utc> = "2026-10-25T10:00:00Z".parse().unwrap();
        let stamp = Wire::timestamp(at);
        assert_eq!(Wire::time(Some(&stamp)), json!("2026-10-25T10:00:00Z"));
        assert_eq!(Wire::instant(Some(&stamp)), Some(at));
        assert_eq!((Wire::time(None), Wire::text("")), (Value::Null, Value::Null));
        assert_eq!(Wire::document(b"").unwrap(), Value::Null);
        assert!(Wire::document(b"{").is_err());
        assert_eq!(Wire::document(br#"{"a":1}"#).unwrap(), json!({"a": 1}));
    }
}
