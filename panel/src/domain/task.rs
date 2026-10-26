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
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl Task {
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
