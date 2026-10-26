use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::AutonomyLevel;

/// Something that happened to a bot, a task or a node, as the control
/// plane's event log keeps it.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Event {
    pub event_id: String,
    #[serde(rename = "type")]
    pub event_type: String,
    #[serde(default)]
    pub tenant_id: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub bot_id: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub node_id: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub task_id: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub execution_id: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub correlation_id: String,
    pub created_at: DateTime<Utc>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub payload: Option<serde_json::Map<String, serde_json::Value>>,
}

/// What an event means at a glance, for its colour and icon.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum EventTone {
    Good,
    Bad,
    Waiting,
    /// A run started, went to a node or came back from one.
    Working,
    Neutral,
}

impl Event {
    /// The event in two or three words.
    pub fn label(&self) -> String {
        match self.event_type.as_str() {
            "bot.registered" => "Bot added".into(),
            "bot.enabled" => "Bot on".into(),
            "bot.paused" => "Bot paused".into(),
            "bot.updated" if self.level_change().is_some() => "Level changed".into(),
            "bot.updated" => "Bot changed".into(),
            "bot.trigger.fired" => "Trigger fired".into(),
            "bot.task.submitted" => "Task in".into(),
            "bot.task.started" => "Started".into(),
            "bot.task.completed" => "Done".into(),
            "bot.task.failed" => "Failed".into(),
            "bot.task.waiting_approval" => "Needs OK".into(),
            "bot.task.decided" => self.decision_label(),
            "bot.task.cancelled" => "Cancelled".into(),
            "bot.task.message" => "You wrote".into(),
            "bot.task.said" => "It answered".into(),
            "bot.task.expired" => "Expired".into(),
            "bot.approval.expired" => "Approval expired".into(),
            "bot.event.emitted" => match self.payload_text("severity").as_deref() {
                Some("critical") => "Alarm to the team".into(),
                Some("warning") => "Warning to the team".into(),
                _ => "Told the team".into(),
            },
            "bot.event.refused" => "Event refused".into(),
            "bot.task.preempted" => "Stopped for urgent".into(),
            "bot.task.delegated" => "Asked a bot".into(),
            "bot.delegation.refused" => "Ask refused".into(),
            "execution.assigned" => "Sent to node".into(),
            "execution.reported" => "Node reported".into(),
            "node.registered" => "Node joined".into(),
            "node.offline" => "Node offline".into(),
            "node.enrolled" => "Node enrolled".into(),
            "node.revoked" => "Node revoked".into(),
            "node.heartbeat" => "Heartbeat".into(),
            other => other.to_string(),
        }
    }

    fn decision_label(&self) -> String {
        match self.payload_text("decision").as_deref() {
            Some("approve") => "Approved".into(),
            Some("deny") => "Denied".into(),
            _ => "Decided".into(),
        }
    }

    pub fn tone(&self) -> EventTone {
        match self.event_type.as_str() {
            "bot.task.completed" | "bot.enabled" | "node.registered" | "node.enrolled" => EventTone::Good,
            "bot.task.failed"
            | "node.offline"
            | "node.revoked"
            | "bot.task.cancelled"
            | "bot.event.refused"
            | "bot.delegation.refused" => EventTone::Bad,
            "bot.task.preempted" => EventTone::Waiting,
            "bot.event.emitted" => match self.payload_text("severity").as_deref() {
                Some("warning") | Some("critical") => EventTone::Waiting,
                _ => EventTone::Neutral,
            },
            "bot.task.waiting_approval" | "bot.paused" | "bot.task.expired" | "bot.approval.expired" => {
                EventTone::Waiting
            }
            "bot.task.started" | "execution.assigned" | "execution.reported" => EventTone::Working,
            "bot.task.decided" => match self.payload_text("decision").as_deref() {
                Some("deny") => EventTone::Bad,
                _ => EventTone::Good,
            },
            _ => EventTone::Neutral,
        }
    }

    /// A payload field as text, when it is a string, a number or a flag.
    pub fn payload_text(&self, key: &str) -> Option<String> {
        match self.payload.as_ref()?.get(key)? {
            serde_json::Value::String(s) if !s.is_empty() => Some(s.clone()),
            serde_json::Value::Number(n) => Some(n.to_string()),
            serde_json::Value::Bool(b) => Some(b.to_string()),
            _ => None,
        }
    }

    /// A turned autonomy dial: the level before and after, in the
    /// panel's words.
    pub fn level_change(&self) -> Option<(AutonomyLevel, AutonomyLevel)> {
        let before = AutonomyLevel::parse(&self.payload_text("previousLevel")?)?;
        let after = AutonomyLevel::parse(&self.payload_text("level")?)?;
        Some((before, after))
    }

    /// Whether people should see it now: a teammate's warning or alarm.
    pub fn needs_attention(&self) -> bool {
        self.event_type == "bot.event.emitted"
            && matches!(
                self.payload_text("severity").as_deref(),
                Some("warning") | Some("critical")
            )
    }

    /// Who an event was for: `quad-1, quad-2`, or `the team`.
    pub fn addressed_to(&self) -> String {
        let to: Vec<&str> = self
            .payload
            .as_ref()
            .and_then(|p| p.get("to"))
            .and_then(|v| v.as_array())
            .map(|a| a.iter().filter_map(|v| v.as_str()).collect())
            .unwrap_or_default();
        if to.is_empty() {
            "the team".into()
        } else {
            to.join(", ")
        }
    }

    /// One useful detail of the payload: the node, the reason, the tool.
    pub fn detail(&self) -> String {
        if let Some((before, after)) = self.level_change() {
            return format!("{} → {}", before.label(), after.label());
        }
        if self.event_type == "bot.event.emitted" {
            let message = self.payload_text("message").unwrap_or_default();
            return format!("{} → {}: {message}", self.bot_id, self.addressed_to());
        }
        if self.event_type == "bot.task.decided"
            && self
                .payload_text("decidedBy")
                .is_some_and(|by| by.starts_with("policy:"))
        {
            return "by its policy, no person needed".into();
        }
        for key in ["nodeId", "reason", "text", "tool", "trigger", "outcome"] {
            if let Some(value) = self.payload_text(key) {
                return value;
            }
        }
        if !self.node_id.is_empty() {
            return self.node_id.clone();
        }
        String::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn event(kind: &str, payload: &str) -> Event {
        serde_json::from_str(&format!(
            r#"{{"eventId":"evt-1","type":"{kind}","tenantId":"default","botId":"bot","taskId":"task-1","createdAt":"2026-10-24T16:04:40Z","payload":{payload}}}"#
        ))
        .unwrap()
    }

    #[test]
    fn a_teams_events_say_who_told_whom_what() {
        let warning = event(
            "bot.event.emitted",
            r#"{"name":"recall","severity":"warning","message":"a ship","to":["quad-1"]}"#,
        );
        assert_eq!(warning.label(), "Warning to the team");
        assert_eq!(warning.detail(), "bot → quad-1: a ship");
        assert!(warning.needs_attention());
        assert_eq!(warning.tone(), EventTone::Waiting);
        let alarm = event("bot.event.emitted", r#"{"severity":"critical","message":"fire"}"#);
        assert_eq!(
            (alarm.label().as_str(), alarm.addressed_to().as_str()),
            ("Alarm to the team", "the team")
        );
        let info = event("bot.event.emitted", r#"{"severity":"info","message":"x"}"#);
        assert_eq!(info.label(), "Told the team");
        assert!(!info.needs_attention());
        assert_eq!(info.tone(), EventTone::Neutral);
        assert_eq!(event("bot.event.refused", "{}").tone(), EventTone::Bad);
        assert_eq!(
            event("bot.task.preempted", r#"{"reason":"preempted by task-2"}"#).label(),
            "Stopped for urgent"
        );
        let by_policy = event(
            "bot.task.decided",
            r#"{"decision":"approve","decidedBy":"policy:autoApprove"}"#,
        );
        assert_eq!(by_policy.detail(), "by its policy, no person needed");
        assert_eq!(event("bot.task.delegated", "{}").label(), "Asked a bot");
        assert_eq!(event("bot.delegation.refused", "{}").tone(), EventTone::Bad);
    }

    #[test]
    fn a_turned_dial_says_from_and_to() {
        let turned = event(
            "bot.updated",
            r#"{"fields":["level"],"previousLevel":"act-within-policy","level":"observe"}"#,
        );
        assert_eq!(turned.label(), "Level changed");
        assert_eq!(turned.detail(), "Act → Observe");
        let other = event("bot.updated", r#"{"fields":["mission"]}"#);
        assert_eq!(other.label(), "Bot changed");
        assert!(other.level_change().is_none());
    }

    #[test]
    fn labels_and_tones() {
        assert_eq!(event("bot.task.completed", "{}").label(), "Done");
        assert_eq!(event("bot.task.completed", "{}").tone(), EventTone::Good);
        assert_eq!(event("bot.task.failed", r#"{"reason":"timeout"}"#).detail(), "timeout");
        assert_eq!(event("bot.task.failed", "{}").tone(), EventTone::Bad);
        assert_eq!(event("bot.task.expired", "{}").label(), "Expired");
        assert_eq!(event("bot.task.expired", "{}").tone(), EventTone::Waiting);
        assert_eq!(event("bot.approval.expired", "{}").label(), "Approval expired");
        assert_eq!(
            event("bot.task.waiting_approval", r#"{"tool":"local_write"}"#).tone(),
            EventTone::Waiting
        );
        let approved = event("bot.task.decided", r#"{"decision":"approve","nodeId":"gx10-ff95"}"#);
        assert_eq!(approved.label(), "Approved");
        assert_eq!(approved.tone(), EventTone::Good);
        assert_eq!(approved.detail(), "gx10-ff95");
        let denied = event("bot.task.decided", r#"{"decision":"deny"}"#);
        assert_eq!(denied.label(), "Denied");
        assert_eq!(denied.tone(), EventTone::Bad);
        assert_eq!(event("bot.task.decided", "{}").label(), "Decided");
        assert_eq!(event("something.new", "{}").label(), "something.new");
        assert_eq!(event("something.new", "{}").tone(), EventTone::Neutral);
        assert_eq!(
            event("bot.task.started", r#"{"count":3}"#)
                .payload_text("count")
                .as_deref(),
            Some("3")
        );
        assert_eq!(event("bot.task.started", r#"{"x":null}"#).payload_text("x"), None);
        assert_eq!(
            event("bot.task.started", r#"{"x":true}"#).payload_text("x").as_deref(),
            Some("true")
        );
        assert_eq!(
            event("bot.task.submitted", r#"{"trigger":{"name":"manual"}}"#).detail(),
            ""
        );
        assert_eq!(event("bot.task.started", "{}").tone(), EventTone::Working);
        assert_eq!(event("bot.task.started", "{}").detail(), "");
        for kind in [
            "bot.registered",
            "bot.enabled",
            "bot.paused",
            "bot.updated",
            "bot.trigger.fired",
            "bot.task.submitted",
            "bot.task.cancelled",
            "execution.assigned",
            "execution.reported",
            "node.registered",
            "node.offline",
            "node.enrolled",
            "node.revoked",
            "node.heartbeat",
        ] {
            assert!(!event(kind, "{}").label().contains('.'), "{kind}");
        }
        let mut on_node = event("node.offline", "{}");
        on_node.node_id = "wrx80".into();
        assert_eq!(on_node.detail(), "wrx80");
    }
}
