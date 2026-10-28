use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::{BotId, ExecutionId, TaskId, TaskStatus, TriggerRef};

/// A task as the control plane reports it: what a bot was asked, where
/// it stands and the executions that ran it.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Task {
    pub id: TaskId,
    pub bot_id: BotId,
    pub objective: String,
    pub status: TaskStatus,
    #[serde(default)]
    pub idempotency_key: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub correlation_id: String,
    #[serde(default)]
    pub trigger: TriggerRef,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub input: Option<serde_json::Value>,
    #[serde(default)]
    pub execution_ids: Vec<ExecutionId>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub failure_reason: String,
    /// `urgent` for a task that went first; empty for normal.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub priority: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl Task {
    /// Whether it went first and stopped the bot's work in progress.
    pub fn is_urgent(&self) -> bool {
        self.priority == "urgent"
    }

    /// Who said what, for a task an event made: `plane · stop.recall: a
    /// ship is entering your zone`.
    pub fn told_by(&self) -> Option<String> {
        let input = self.input.as_ref()?;
        let from = input.get("from")?.as_str()?;
        let message = input.get("message")?.as_str()?;
        match input.get("name").and_then(|n| n.as_str()) {
            Some(name) => Some(format!("{from} · {name}: {message}")),
            None => Some(format!("{from}: {message}")),
        }
    }

    /// What the task knew when it started: the notes of the events its
    /// bot heard, oldest first, as `recepcion · info.turno: shift at six`.
    pub fn knew(&self) -> Vec<String> {
        let Some(notes) = self
            .input
            .as_ref()
            .and_then(|i| i.get("notes"))
            .and_then(|n| n.as_array())
        else {
            return Vec::new();
        };
        notes
            .iter()
            .filter_map(|note| {
                let from = note.get("from")?.as_str()?;
                let name = note.get("name")?.as_str()?;
                let message = note.get("message")?.as_str()?;
                Some(format!("{from} · {name}: {message}"))
            })
            .collect()
    }

    /// The objective's first line, cut to `max` characters with an ellipsis.
    pub fn headline(&self, max: usize) -> String {
        let first = self.objective.lines().next().unwrap_or_default();
        if first.chars().count() <= max {
            return first.to_string();
        }
        let cut: String = first.chars().take(max.saturating_sub(1)).collect();
        format!("{cut}…")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    pub(crate) fn sample() -> Task {
        serde_json::from_str(
            r#"{"id":"task-68ed","botId":"bot-wrx80","objective":"List the files in your workspace and say how many there are.\nThen stop.",
            "status":"Completed","idempotencyKey":"wrx80-first-run-1","correlationId":"trace-e470",
            "trigger":{"type":"manual","name":"manual"},"executionIds":["exec-4a92"],
            "createdAt":"2026-10-24T22:32:26Z","updatedAt":"2026-10-24T22:32:33Z"}"#,
        )
        .unwrap()
    }

    #[test]
    fn says_who_told_it_and_whether_it_was_urgent() {
        let mut task = sample();
        assert!(!task.is_urgent() && task.told_by().is_none());
        task.priority = "urgent".into();
        task.input = Some(serde_json::json!({"from": "plane", "message": "a ship"}));
        assert!(task.is_urgent());
        assert_eq!(task.told_by().as_deref(), Some("plane: a ship"));
        task.input = Some(
            serde_json::json!({"from": "plane", "name": "stop.recall", "message": "a ship",
            "notes": [{"from": "recepcion", "name": "info.turno", "message": "shift", "at": "2026-10-28T10:00:00Z"}, {"bad": 1}]}),
        );
        assert_eq!(task.told_by().as_deref(), Some("plane · stop.recall: a ship"));
        assert_eq!(task.knew(), vec!["recepcion · info.turno: shift"]);
        assert!(sample().knew().is_empty());
    }

    #[test]
    fn reads_the_api_json() {
        let task = sample();
        assert_eq!(task.status, TaskStatus::Completed);
        assert_eq!(task.execution_ids.len(), 1);
        assert_eq!(task.trigger.describe(), "manual");
        assert!(task.input.is_none());
        assert!(task.failure_reason.is_empty());
    }

    #[test]
    fn cuts_the_headline() {
        let task = sample();
        assert_eq!(
            task.headline(100),
            "List the files in your workspace and say how many there are."
        );
        assert_eq!(task.headline(10), "List the …");
    }
}
