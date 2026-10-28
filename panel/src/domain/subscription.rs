use serde::{Deserialize, Serialize};

use super::DeliveryMode;
use super::DomainError;
use super::identifier::validate_identifier;

/// A topic a bot listens to in its teams, as the control plane keeps it.
/// Every field the control plane leaves out stays out when the panel
/// writes the list back, so a bot's ask it keeps is the same ask.
#[derive(Debug, Clone, PartialEq, Eq, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Subscription {
    pub topic: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub mode: String,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub from: Vec<String>,
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub urgent: bool,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub objective: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub status: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub asked_by: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub why: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub asked_at: Option<String>,
}

/// The most topics a bot may listen to.
pub const MAX_SUBSCRIPTIONS: usize = 16;

impl Subscription {
    /// A topic the operator makes the bot listen to: a mode, or the
    /// kind's default when none; whom from, or anyone.
    pub fn new(topic: &str, mode: Option<DeliveryMode>, from: &str) -> Result<Self, DomainError> {
        let topic = topic.trim();
        validate_topic(topic)?;
        let mut senders = Vec::new();
        for sender in from.split([',', ' ']).map(str::trim).filter(|s| !s.is_empty()) {
            validate_identifier("from", sender)?;
            if !senders.iter().any(|s| s == sender) {
                senders.push(sender.to_string());
            }
        }
        Ok(Self {
            topic: topic.to_string(),
            mode: mode.map(|m| m.name().to_string()).unwrap_or_default(),
            from: senders,
            ..Self::default()
        })
    }

    /// A bot's ask a person has not approved: it hears nothing.
    pub fn is_pending(&self) -> bool {
        self.status == "pending"
    }

    /// What an event it hears does: its own mode, or the kind's.
    pub fn mode(&self) -> DeliveryMode {
        DeliveryMode::parse(&self.mode).unwrap_or_else(|| DeliveryMode::by_kind(&self.topic))
    }

    /// Whom from: `recepcion, operator`, or `anyone`.
    pub fn senders(&self) -> String {
        if self.from.is_empty() {
            "anyone".into()
        } else {
            self.from.join(", ")
        }
    }

    /// The same ask, approved.
    pub fn approved(&self) -> Self {
        Self {
            status: String::new(),
            ..self.clone()
        }
    }
}

/// A name or a pattern: dot-separated segments of lowercase letters,
/// digits and `-`, or `*` and `**`; at most 64 characters.
pub fn validate_topic(topic: &str) -> Result<(), DomainError> {
    if topic.is_empty() || topic.len() > 64 {
        return Err(DomainError::new("topic", "must be 1 to 64 characters"));
    }
    for segment in topic.split('.') {
        let ok = segment == "*"
            || segment == "**"
            || (!segment.is_empty()
                && segment
                    .chars()
                    .all(|c| c.is_ascii_lowercase() || c.is_ascii_digit() || c == '-'));
        if !ok {
            return Err(DomainError::new(
                "topic",
                "uses segments of lowercase letters, digits and '-' separated by '.', or * and **",
            ));
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads_and_writes_what_the_control_plane_keeps() {
        let raw = r#"{"topic":"error.*","status":"pending","askedBy":"almacen","why":"para reintentar","askedAt":"2026-10-28T10:00:00Z"}"#;
        let ask: Subscription = serde_json::from_str(raw).unwrap();
        assert!(ask.is_pending());
        assert_eq!(serde_json::to_string(&ask).unwrap(), raw);
        assert_eq!(ask.mode(), DeliveryMode::Task);
        assert_eq!(ask.senders(), "anyone");
        let approved = ask.approved();
        assert!(!approved.is_pending() && approved.asked_by == "almacen");
        assert_eq!(
            serde_json::to_string(
                &Subscription::new("pedido.*", Some(DeliveryMode::Task), "recepcion, operator").unwrap()
            )
            .unwrap(),
            r#"{"topic":"pedido.*","mode":"task","from":["recepcion","operator"]}"#
        );
        let plain = Subscription::new(" warn.* ", None, "").unwrap();
        assert_eq!(plain.mode(), DeliveryMode::Message);
        assert_eq!(serde_json::to_string(&plain).unwrap(), r#"{"topic":"warn.*"}"#);
        assert!(Subscription::new("Bad", None, "").is_err());
        assert!(Subscription::new("a..b", None, "").is_err());
        assert!(Subscription::new(&"a".repeat(65), None, "").is_err());
        assert!(Subscription::new("a", None, "Not Ok").is_err());
        assert_eq!(Subscription::new("a", None, "x x").unwrap().from, vec!["x"]);
    }
}
