use serde::{Deserialize, Serialize};

/// The ids that tie an execution to its bot, task and trace, and whether
/// the runtime carried them into its own records.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Correlation {
    #[serde(default)]
    pub bot_id: String,
    #[serde(default)]
    pub task_id: String,
    #[serde(default)]
    pub execution_id: String,
    #[serde(default)]
    pub trace_id: String,
    #[serde(default)]
    pub carried: bool,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub path: String,
}
