use chrono::{DateTime, Utc};
use serde::Deserialize;

use super::TaskId;

/// The control plane's answer to a message for a running task: the task,
/// the execution the node hands it to, and when.
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SentTaskMessage {
    pub task_id: TaskId,
    pub execution_id: String,
    pub created_at: DateTime<Utc>,
}
