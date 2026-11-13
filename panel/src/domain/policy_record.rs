use serde::{Deserialize, Serialize};

/// The policy an execution ran under and whether the runtime enforced it.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PolicyRecord {
    /// The level the execution ran at: the bot's.
    #[serde(default)]
    pub level: String,
    /// The tools it was given: the bot's, with the node's own tools.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub allowed_tools: Vec<String>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub runtime_level: String,
    #[serde(default)]
    pub enforced: bool,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub path: String,
}
