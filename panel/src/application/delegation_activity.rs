use chrono::{DateTime, Utc};

use crate::domain::TaskId;

/// A run asked another bot: whom, the child task the control plane
/// created, or why it refused.
#[derive(Debug, Clone, PartialEq)]
pub struct DelegationActivity {
    pub bot: String,
    pub task: Option<TaskId>,
    pub refused: String,
    pub at: DateTime<Utc>,
}
