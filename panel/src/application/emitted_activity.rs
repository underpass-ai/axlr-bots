use chrono::{DateTime, Utc};

use crate::domain::Event;

/// An event a run emitted to its team: its name, its message and the
/// bots it reached, as the control plane recorded it on the task.
#[derive(Debug, Clone, PartialEq)]
pub struct EmittedActivity {
    pub name: String,
    pub message: String,
    /// The bots it went to: those it was delivered to, else those it
    /// named; none when it went to the whole team and nobody took it.
    pub to: Vec<String>,
    pub at: DateTime<Utc>,
}

impl EmittedActivity {
    /// The event as the run emitted it, from a `bot.event.emitted` of
    /// the task; none for an event without a name.
    pub fn of(event: &Event) -> Option<Self> {
        let name = event.payload_text("name")?;
        let names = |key: &str, field: Option<&str>| -> Vec<String> {
            event
                .payload
                .as_ref()
                .and_then(|p| p.get(key))
                .and_then(|v| v.as_array())
                .map(|list| {
                    list.iter()
                        .filter_map(|v| match field {
                            Some(field) => v.get(field).and_then(|f| f.as_str()),
                            None => v.as_str(),
                        })
                        .filter(|name| !name.is_empty())
                        .map(str::to_string)
                        .collect()
                })
                .unwrap_or_default()
        };
        let delivered = names("deliveries", Some("botId"));
        let to = if delivered.is_empty() {
            names("to", None)
        } else {
            delivered
        };
        Some(Self {
            name,
            message: event.payload_text("message").unwrap_or_default(),
            to,
            at: event.created_at,
        })
    }

    /// Whom it reached, `→ worker-1, worker-2`; empty when it named none.
    pub fn recipients(&self) -> String {
        if self.to.is_empty() {
            String::new()
        } else {
            format!("→ {}", self.to.join(", "))
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn emitted(payload: &str) -> Event {
        serde_json::from_str(&format!(
            r#"{{"eventId":"e1","type":"bot.event.emitted","tenantId":"t","botId":"lead","taskId":"task-1","createdAt":"2026-11-02T10:00:00Z","payload":{payload}}}"#
        ))
        .unwrap()
    }

    #[test]
    fn names_the_bots_it_reached_by_delivery_then_by_address() {
        let delivered = EmittedActivity::of(&emitted(
            r#"{"name":"item.assigned","message":"item-1 · planned","to":["a"],"deliveries":[{"botId":"worker"},{"botId":""}]}"#,
        ))
        .unwrap();
        assert_eq!(
            (
                delivered.name.as_str(),
                delivered.message.as_str(),
                delivered.to.clone()
            ),
            ("item.assigned", "item-1 · planned", vec!["worker".to_string()])
        );
        assert_eq!(delivered.recipients(), "→ worker");
        let named = EmittedActivity::of(&emitted(r#"{"name":"x","to":["a","b"]}"#)).unwrap();
        assert_eq!((named.to.len(), named.message.as_str()), (2, ""));
        let team = EmittedActivity::of(&emitted(r#"{"name":"x","message":"m"}"#)).unwrap();
        assert_eq!(team.recipients(), "");
        assert_eq!(EmittedActivity::of(&emitted(r#"{"message":"m"}"#)), None);
    }
}
