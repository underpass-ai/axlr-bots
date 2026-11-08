use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/// One person's approval of a call that waits for two: who approved it,
/// as their decision named them, and when.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ApprovalMark {
    pub actor: String,
    pub at: DateTime<Utc>,
}
