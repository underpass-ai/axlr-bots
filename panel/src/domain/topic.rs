use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/// One topic of a team, as the control plane derives it: who publishes
/// it, who listens and how, when it was last heard of.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Topic {
    pub name: String,
    #[serde(default)]
    pub publishers: Vec<String>,
    #[serde(default)]
    pub declared_publishers: Vec<String>,
    #[serde(default)]
    pub listeners: Vec<TopicListener>,
    #[serde(default)]
    pub last_at: Option<DateTime<Utc>>,
    #[serde(default)]
    pub count: u32,
    #[serde(default)]
    pub nobody_listens: bool,
}

/// A member that listens to a topic.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct TopicListener {
    pub bot_id: String,
    #[serde(default)]
    pub topic: String,
    #[serde(default)]
    pub mode: String,
    #[serde(default)]
    pub status: String,
}

impl Topic {
    /// Who publishes it: seen, else declared, else nobody yet.
    pub fn publishers_text(&self) -> String {
        let all: Vec<&str> = self
            .publishers
            .iter()
            .chain(self.declared_publishers.iter())
            .map(String::as_str)
            .collect();
        if all.is_empty() {
            "nobody yet".into()
        } else {
            let mut unique: Vec<&str> = Vec::new();
            for name in all {
                if !unique.contains(&name) {
                    unique.push(name);
                }
            }
            unique.join(", ")
        }
    }

    /// Who listens and how: `almacen (task), supervisor (note, asked)`.
    pub fn listeners_text(&self) -> String {
        if self.listeners.is_empty() {
            return "nobody".into();
        }
        self.listeners
            .iter()
            .map(|l| {
                let mode = if l.mode.is_empty() { "by kind" } else { l.mode.as_str() };
                if l.status == "pending" {
                    format!("{} ({mode}, asked)", l.bot_id)
                } else {
                    format!("{} ({mode})", l.bot_id)
                }
            })
            .collect::<Vec<_>>()
            .join(", ")
    }
}

/// The operator's event on a team.
#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Publication {
    pub name: String,
    pub message: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub data: Option<serde_json::Value>,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub to: Vec<String>,
    pub idempotency_key: String,
}

/// What the control plane says to the operator's event.
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Published {
    pub event_id: String,
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub deliveries: Vec<serde_json::Value>,
    #[serde(default)]
    pub skipped: Vec<serde_json::Value>,
    #[serde(default)]
    pub created: bool,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn says_who_publishes_and_who_listens() {
        let topic: Topic = serde_json::from_str(
            r#"{"name":"pedido.nuevo","publishers":["recepcion"],"declaredPublishers":["recepcion","caja"],
            "listeners":[{"botId":"almacen","topic":"pedido.*","mode":"task"},{"botId":"jefe","topic":"**","status":"pending"}],
            "lastAt":"2026-10-28T10:00:00Z","count":2}"#,
        )
        .unwrap();
        assert_eq!(topic.publishers_text(), "recepcion, caja");
        assert_eq!(topic.listeners_text(), "almacen (task), jefe (by kind, asked)");
        let bare: Topic = serde_json::from_str(r#"{"name":"info.turno","nobodyListens":true}"#).unwrap();
        assert_eq!(bare.publishers_text(), "nobody yet");
        assert_eq!(bare.listeners_text(), "nobody");
        assert!(bare.nobody_listens);
        let published: Published =
            serde_json::from_str(r#"{"eventId":"evt-1","deliveries":[{}],"skipped":[],"created":true}"#).unwrap();
        assert_eq!(published.deliveries.len(), 1);
        let publication = Publication {
            name: "stop.all".into(),
            message: "Everyone stop".into(),
            data: None,
            to: Vec::new(),
            idempotency_key: "k".into(),
        };
        assert_eq!(
            serde_json::to_string(&publication).unwrap(),
            r#"{"name":"stop.all","message":"Everyone stop","idempotencyKey":"k"}"#
        );
    }
}
