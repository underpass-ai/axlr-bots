use serde_json::{Value, json};

use super::proto;
use super::wire::Wire;
use crate::application::PanelError;
use crate::domain::Event;

/// A timeline entry of the control plane, into the panel's.
pub(super) struct EventMapper;

impl EventMapper {
    pub fn event(message: &proto::Event) -> Result<Event, PanelError> {
        Wire::read(Self::shape(message), "event")
    }

    pub fn events(messages: &[proto::Event]) -> Result<Vec<Event>, PanelError> {
        messages.iter().map(Self::event).collect()
    }

    fn shape(event: &proto::Event) -> Value {
        json!({
            "eventId": event.event_id,
            "type": event.r#type,
            "tenantId": event.tenant_id,
            "botId": event.bot_id,
            "nodeId": event.node_id,
            "groupName": event.group_name,
            "taskId": event.task_id,
            "executionId": event.execution_id,
            "correlationId": event.correlation_id,
            "createdAt": Wire::time(event.created_at.as_ref()),
            "payload": event.payload.as_ref().map(Wire::object),
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn progress_numbers_survive_the_struct() {
        use prost_types::value::Kind;
        let number = |n: f64| prost_types::Value {
            kind: Some(Kind::NumberValue(n)),
        };
        let text = |s: &str| prost_types::Value {
            kind: Some(Kind::StringValue(s.into())),
        };
        let item = prost_types::Struct {
            fields: [
                ("kind".to_string(), text("tool.ended")),
                ("durationMs".to_string(), number(1500.0)),
                ("tool".to_string(), text("local_read")),
            ]
            .into_iter()
            .collect(),
        };
        let payload = prost_types::Struct {
            fields: [
                ("dropped".to_string(), number(2.0)),
                (
                    "items".to_string(),
                    prost_types::Value {
                        kind: Some(Kind::ListValue(prost_types::ListValue {
                            values: vec![prost_types::Value {
                                kind: Some(Kind::StructValue(item)),
                            }],
                        })),
                    },
                ),
            ]
            .into_iter()
            .collect(),
        };
        let event = EventMapper::event(&proto::Event {
            event_id: "e1".into(),
            r#type: "bot.task.progress".into(),
            task_id: "task-1".into(),
            created_at: Some(Wire::timestamp("2026-10-25T10:00:00Z".parse().unwrap())),
            payload: Some(payload),
            ..Default::default()
        })
        .unwrap();
        assert_eq!(
            event.progress().len(),
            1,
            "a progress item must not vanish in the float"
        );
        assert_eq!(event.progress()[0].duration_ms, Some(1500));
        assert_eq!(event.payload.unwrap()["dropped"].as_u64(), Some(2));
    }

    #[test]
    fn an_event_without_its_instant_is_unreadable() {
        let broken = EventMapper::event(&proto::Event {
            event_id: "e1".into(),
            r#type: "x".into(),
            ..Default::default()
        });
        assert!(matches!(broken, Err(PanelError::Unreadable(_))));
    }
}
