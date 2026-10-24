use serde::Deserialize;

use super::Task;

/// The answer to a submission: the task, and whether this call created it
/// or found the one its key already named.
#[derive(Debug, Clone, PartialEq, Deserialize)]
pub struct SubmittedTask {
    pub created: bool,
    pub task: Task,
}
