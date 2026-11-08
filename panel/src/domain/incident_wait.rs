use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/// The approval an incident's current task waits for, or last waited
/// for: the call, its tool, the deadline and, once it ended, how.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct IncidentWait {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub call_id: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub tool: String,
    pub deadline: DateTime<Utc>,
    /// approved, denied, expired or cancelled; empty while it waits.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub outcome: String,
}

impl IncidentWait {
    /// Whether nobody has decided it yet.
    pub fn is_open(&self) -> bool {
        self.outcome.is_empty()
    }
}
