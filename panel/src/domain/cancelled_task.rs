use serde::Deserialize;

use super::{Execution, Task};

/// The answer to a cancel: the task, the execution it stopped and whether
/// anything changed.
#[derive(Debug, Clone, PartialEq, Deserialize)]
pub struct CancelledTask {
    pub task: Task,
    #[serde(default)]
    pub execution: Option<Execution>,
    pub changed: bool,
}
