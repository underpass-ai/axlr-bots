use crate::domain::{Bot, Node, Overview, Task, TaskStatus};

/// Everything the panel shows at once, read in one refresh: the overview's
/// counts, the nodes, the bots and the newest tasks.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct Fleet {
    pub overview: Overview,
    pub nodes: Vec<Node>,
    pub bots: Vec<Bot>,
    pub tasks: Vec<Task>,
}

impl Fleet {
    /// The tasks waiting for a person, newest first.
    pub fn waiting(&self) -> Vec<&Task> {
        self.tasks
            .iter()
            .filter(|t| t.status == TaskStatus::WaitingApproval)
            .collect()
    }

    /// The tasks running or waiting to start, newest first.
    pub fn active(&self) -> Vec<&Task> {
        self.tasks
            .iter()
            .filter(|t| matches!(t.status, TaskStatus::Running | TaskStatus::Pending))
            .collect()
    }
}
