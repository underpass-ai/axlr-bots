use serde::{Deserialize, Serialize};

/// A file an execution produced, such as the task's report.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Artifact {
    pub name: String,
    #[serde(default)]
    pub media_type: String,
    #[serde(default)]
    pub reference: String,
}
