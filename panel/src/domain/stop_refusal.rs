use serde::Deserialize;

/// A task a stop could not cancel, and why.
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct StopRefusal {
    pub task_id: String,
    pub reason: String,
}
