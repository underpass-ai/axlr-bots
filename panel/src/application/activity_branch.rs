use super::ActivityRun;
use crate::domain::{Task, Usage};

/// One task of an activity tree as the screen shows it, in the order the
/// tree reads: the task, how deep it is, its executions, what its own
/// executions used and what it and everything it delegated used.
#[derive(Debug, Clone, PartialEq)]
pub struct ActivityBranch {
    pub depth: usize,
    pub task: Task,
    pub runs: Vec<ActivityRun>,
    pub own: Usage,
    pub subtree: Usage,
}
