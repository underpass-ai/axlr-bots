use serde::{Deserialize, Serialize};

/// The policy an execution ran under and whether the runtime enforced it.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PolicyRecord {
    #[serde(default)]
    pub level: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub runtime_level: String,
    #[serde(default)]
    pub enforced: bool,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub path: String,
}
