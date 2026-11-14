use serde_json::{Value, json};

use super::proto;
use super::wire::Wire;
use crate::application::PanelError;
use crate::domain::{Publication, Published, Topic};

/// A team's topics and the events the operator publishes on it.
pub(super) struct GroupMapper;

impl GroupMapper {
    pub fn topics(messages: &[proto::GroupTopic]) -> Result<Vec<Topic>, PanelError> {
        messages
            .iter()
            .map(|t| {
                Wire::read(
                    json!({
                        "name": t.name,
                        "publishers": t.publishers,
                        "declaredPublishers": t.declared_publishers,
                        "listeners": t.listeners.iter().map(|l| json!({
                            "botId": l.bot_id, "topic": l.topic, "mode": l.mode, "status": l.status,
                        })).collect::<Vec<_>>(),
                        "lastAt": Wire::time(t.last_at.as_ref()),
                        "count": t.count.max(0),
                        "nobodyListens": t.nobody_listens,
                    }),
                    "topic",
                )
            })
            .collect()
    }

    pub fn publication(team: &str, publication: &Publication) -> Result<proto::PublishGroupEventRequest, PanelError> {
        let data = match &publication.data {
            None | Some(Value::Null) => None,
            Some(Value::Object(map)) => Some(Wire::structure(map)),
            Some(_) => return Err(PanelError::Invalid("the event's data must be an object".into())),
        };
        Ok(proto::PublishGroupEventRequest {
            name: team.to_string(),
            tenant: String::new(),
            event_name: publication.name.clone(),
            message: publication.message.clone(),
            data,
            to: publication.to.clone(),
            idempotency_key: publication.idempotency_key.clone(),
        })
    }

    pub fn published(answer: &proto::PublishGroupEventResponse) -> Result<Published, PanelError> {
        Wire::read(
            json!({
                "eventId": answer.event_id,
                "name": answer.name,
                "deliveries": answer.deliveries.iter().map(|d| json!({
                    "botId": d.bot_id, "mode": d.mode, "taskId": d.task_id, "messageId": d.message_id,
                    "trigger": d.trigger, "created": d.created, "urgent": d.urgent,
                    "degraded": d.degraded, "coalescedInto": d.coalesced_into,
                })).collect::<Vec<_>>(),
                "skipped": answer.skipped.iter().map(|s| json!({
                    "botId": s.bot_id, "reason": s.reason, "message": s.message,
                })).collect::<Vec<_>>(),
                "created": answer.created,
            }),
            "published event",
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads_topics_with_their_listeners() {
        let topics = GroupMapper::topics(&[proto::GroupTopic {
            name: "info.turno".into(),
            publishers: vec!["bot-a".into()],
            listeners: vec![proto::TopicListener {
                bot_id: "bot-b".into(),
                mode: "task".into(),
                ..Default::default()
            }],
            count: 3,
            last_at: Some(Wire::timestamp("2026-10-25T09:00:00Z".parse().unwrap())),
            ..Default::default()
        }])
        .unwrap();
        assert_eq!((topics[0].count, topics[0].listeners[0].bot_id.as_str()), (3, "bot-b"));
        assert!(topics[0].last_at.is_some());
    }

    #[test]
    fn publishes_with_its_data_and_reads_the_deliveries() {
        let publication = Publication {
            name: "stop.halt".into(),
            message: "deploy".into(),
            data: Some(json!({"n": 2})),
            to: vec!["bot-a".into()],
            idempotency_key: "k1".into(),
        };
        let request = GroupMapper::publication("team-a", &publication).unwrap();
        assert_eq!(
            (
                request.name.as_str(),
                request.event_name.as_str(),
                request.idempotency_key.as_str()
            ),
            ("team-a", "stop.halt", "k1")
        );
        assert_eq!(Wire::object(request.data.as_ref().unwrap())["n"].as_u64(), Some(2));
        let mut bad = publication;
        bad.data = Some(json!([1]));
        assert!(GroupMapper::publication("team-a", &bad).is_err());
        let published = GroupMapper::published(&proto::PublishGroupEventResponse {
            event_id: "evt-1".into(),
            created: true,
            deliveries: vec![proto::EventDelivery {
                bot_id: "bot-a".into(),
                ..Default::default()
            }],
            ..Default::default()
        })
        .unwrap();
        assert_eq!((published.created, published.deliveries.len()), (true, 1));
    }
}
