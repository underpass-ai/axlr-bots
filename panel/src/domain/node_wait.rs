use chrono::{DateTime, Utc};

use super::Event;

/// What a decision queued for a node waits for, as the control plane
/// tells it: in the decided event when the node is busy or not Online,
/// and in a `node.offline` event of the task's timeline, once per state
/// of the node, while the node stays away.
#[derive(Debug, Clone, PartialEq)]
pub struct NodeWait {
    pub node: String,
    /// The node's state, `Offline`, `Registered` or `gone`; empty when it
    /// is Online and busy.
    pub status: String,
    /// When the node was last seen, for a node that went away.
    pub since: Option<DateTime<Utc>>,
    /// The tasks the node runs, for a busy node.
    pub busy_with: Vec<String>,
    /// The control plane's own sentence, in UTC.
    pub said: String,
    /// The execution that waits.
    pub execution: String,
}

impl NodeWait {
    /// What the event says the decision waits for; none for an event
    /// that does not say it.
    pub fn of(event: &Event) -> Option<Self> {
        if !matches!(event.event_type.as_str(), "bot.task.decided" | "node.offline") {
            return None;
        }
        let said = event.payload_text("waiting")?;
        let busy_with = event
            .payload
            .as_ref()
            .and_then(|p| p.get("busyWith"))
            .and_then(|v| v.as_array())
            .map(|tasks| tasks.iter().filter_map(|t| t.as_str().map(str::to_string)).collect())
            .unwrap_or_default();
        Some(Self {
            node: event.payload_text("nodeId").unwrap_or_else(|| event.node_id.clone()),
            status: event.payload_text("nodeStatus").unwrap_or_default(),
            since: event.payload_text("lastSeenAt").and_then(|at| at.parse().ok()),
            busy_with,
            said,
            execution: event
                .payload_text("executionId")
                .unwrap_or_else(|| event.execution_id.clone()),
        })
    }

    /// What the task waits for now, from its timeline, oldest first: the
    /// newest wait told, while its execution has not gone to a node, told
    /// anything or ended since.
    pub fn current<'a>(events: impl IntoIterator<Item = &'a Event>) -> Option<Self> {
        let mut wait: Option<Self> = None;
        for event in events {
            if let Some(told) = Self::of(event) {
                wait = Some(told);
            } else if wait.as_ref().is_some_and(|w| {
                event.execution_id == w.execution
                    && matches!(
                        event.event_type.as_str(),
                        "execution.assigned" | "execution.reported" | "bot.task.progress"
                    )
            }) || matches!(
                event.event_type.as_str(),
                "bot.task.completed" | "bot.task.failed" | "bot.task.cancelled" | "bot.task.expired"
            ) {
                wait = None;
            }
        }
        wait
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn event(id: &str, kind: &str, payload: serde_json::Value) -> Event {
        serde_json::from_value(serde_json::json!({
            "eventId": id, "type": kind, "tenantId": "t", "botId": "watcher", "taskId": "task-1",
            "executionId": "exec-2", "createdAt": "2026-11-05T10:13:37Z", "payload": payload
        }))
        .unwrap()
    }

    fn offline() -> Event {
        event(
            "e2",
            "node.offline",
            serde_json::json!({"executionId": "exec-2", "nodeId": "node-1", "nodeStatus": "Offline",
                "lastSeenAt": "2026-11-05T10:09:26.44Z", "waiting": "waiting for node node-1, offline since 10:09 UTC"}),
        )
    }

    #[test]
    fn reads_what_the_decision_waits_for() {
        let wait = NodeWait::of(&offline()).unwrap();
        assert_eq!((wait.node.as_str(), wait.status.as_str()), ("node-1", "Offline"));
        assert_eq!(wait.since, Some("2026-11-05T10:09:26.44Z".parse().unwrap()));
        assert_eq!(wait.execution, "exec-2");
        let busy = NodeWait::of(&event(
            "e1",
            "bot.task.decided",
            serde_json::json!({"decision": "approve", "nodeId": "node-1", "busyWith": ["task-9"],
                "waiting": "waiting for node node-1, busy with task task-9"}),
        ))
        .unwrap();
        assert_eq!(busy.busy_with, ["task-9"]);
        assert_eq!(busy.since, None);
        assert!(
            NodeWait::of(&event(
                "e1",
                "bot.task.decided",
                serde_json::json!({"decision": "approve"})
            ))
            .is_none()
        );
        assert!(NodeWait::of(&event("e1", "bot.task.failed", serde_json::json!({"waiting": "x"}))).is_none());
    }

    #[test]
    fn a_wait_ends_when_its_run_goes_to_a_node_or_the_task_ends() {
        let mut events = vec![event("e0", "bot.task.started", serde_json::json!({})), offline()];
        assert_eq!(NodeWait::current(&events).unwrap().node, "node-1");
        // Another run's event does not end it.
        let mut other = event("e3", "execution.reported", serde_json::json!({}));
        other.execution_id = "exec-1".into();
        events.push(other);
        assert!(NodeWait::current(&events).is_some());
        events.push(event("e4", "execution.assigned", serde_json::json!({})));
        assert!(NodeWait::current(&events).is_none());
        assert!(NodeWait::current(&[offline(), event("e5", "bot.task.failed", serde_json::json!({}))]).is_none());
    }
}
