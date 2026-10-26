use super::WaitingCall;
use crate::domain::{Bot, Node, Overview, PendingDecision, Task, TaskId, TaskStatus};

/// Everything the panel shows at once, read in one refresh: the overview's
/// counts, the nodes, the bots and the newest tasks.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct Fleet {
    pub overview: Overview,
    pub nodes: Vec<Node>,
    pub bots: Vec<Bot>,
    pub tasks: Vec<Task>,
    /// The calls the newest waiting tasks stopped on.
    pub calls: Vec<WaitingCall>,
}

impl Fleet {
    /// The tasks waiting for a person, newest first.
    pub fn waiting(&self) -> Vec<&Task> {
        self.tasks
            .iter()
            .filter(|t| t.status == TaskStatus::WaitingApproval)
            .collect()
    }

    /// The call a waiting task stopped on, when the refresh read it.
    pub fn call_of(&self, task: &TaskId) -> Option<&PendingDecision> {
        self.calls.iter().find(|c| &c.task == task).map(|c| &c.call)
    }

    /// The tasks running or waiting to start, newest first.
    pub fn active(&self) -> Vec<&Task> {
        self.tasks
            .iter()
            .filter(|t| matches!(t.status, TaskStatus::Running | TaskStatus::Pending))
            .collect()
    }
}
