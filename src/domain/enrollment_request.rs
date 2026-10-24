use std::collections::BTreeMap;

use serde::Serialize;

use super::{DomainError, NodeId};

/// What a new host's one-time token is for: the node id it will take (or
/// one the control plane picks), its labels and how long the token lives.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct EnrollmentRequest {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub node_id: Option<NodeId>,
    #[serde(skip_serializing_if = "BTreeMap::is_empty")]
    pub labels: BTreeMap<String, String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub ttl: Option<String>,
}

impl EnrollmentRequest {
    /// Reads the form: an optional node id, labels as `k=v, k=v` and a
    /// lifetime such as `1h` or `30m`.
    pub fn parse(node_id: &str, labels: &str, ttl: &str) -> Result<Self, DomainError> {
        let node_id = match node_id.trim() {
            "" => None,
            raw => Some(NodeId::new(raw)?),
        };
        let mut parsed = BTreeMap::new();
        for pair in labels.split(',').map(str::trim).filter(|p| !p.is_empty()) {
            let (key, value) = pair.split_once('=').ok_or_else(|| {
                DomainError::new("labels", "must be key=value, separated by commas")
            })?;
            let key = super::identifier::validate_identifier("label key", key)?;
            let value = value.trim();
            if value.is_empty() {
                return Err(DomainError::new("labels", "need a value after '='"));
            }
            parsed.insert(key, value.to_string());
        }
        let ttl = match ttl.trim() {
            "" => None,
            raw => Some(validate_duration(raw)?),
        };
        Ok(Self {
            node_id,
            labels: parsed,
            ttl,
        })
    }
}

/// A Go duration as the control plane reads it: numbers with s, m or h.
fn validate_duration(raw: &str) -> Result<String, DomainError> {
    let invalid = || DomainError::new("lifetime", "must be like 30m, 1h or 2h30m");
    let mut digits = String::new();
    let mut parts = 0;
    for c in raw.chars() {
        if c.is_ascii_digit() {
            digits.push(c);
        } else if matches!(c, 'h' | 'm' | 's') && !digits.is_empty() {
            digits.clear();
            parts += 1;
        } else {
            return Err(invalid());
        }
    }
    if !digits.is_empty() || parts == 0 {
        return Err(invalid());
    }
    Ok(raw.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads_the_form() {
        let request =
            EnrollmentRequest::parse(" web-2 ", "zone=eu, gpu = rtx3090", "2h30m").unwrap();
        assert_eq!(request.node_id.unwrap().as_str(), "web-2");
        assert_eq!(
            request.labels.get("gpu").map(String::as_str),
            Some("rtx3090")
        );
        assert_eq!(request.ttl.as_deref(), Some("2h30m"));
        let empty = EnrollmentRequest::parse("", "", "").unwrap();
        assert_eq!(empty, EnrollmentRequest::default());
        assert_eq!(serde_json::to_string(&empty).unwrap(), "{}");
    }

    #[test]
    fn refuses_bad_input() {
        assert!(EnrollmentRequest::parse("Web 2", "", "").is_err());
        assert!(EnrollmentRequest::parse("", "zone", "").is_err());
        assert!(EnrollmentRequest::parse("", "Zone=eu", "").is_err());
        assert!(EnrollmentRequest::parse("", "zone=", "").is_err());
        for ttl in ["soon", "1", "h", "1d", "-1h"] {
            assert!(EnrollmentRequest::parse("", "", ttl).is_err(), "{ttl}");
        }
    }
}
