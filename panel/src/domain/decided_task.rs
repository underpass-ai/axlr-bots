use serde::Deserialize;

use super::{Execution, Task};

/// The answer to a decision: the task and the execution that carries it.
#[derive(Debug, Clone, PartialEq, Deserialize)]
pub struct DecidedTask {
    pub task: Task,
    pub execution: Execution,
}
