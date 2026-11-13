use crate::domain::{BotId, NodeId, TaskId};

/// One row a bulk act goes to, with what its call needs.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum BulkTarget {
    Bot(BotId),
    /// A task, and the call it waits on when the panel knows it, so the
    /// control plane refuses a decision on a call that has moved on; its
    /// label says it to a person: the bot and the task's first words.
    Task {
        id: TaskId,
        call: Option<String>,
        label: String,
    },
    Node(NodeId),
}

impl BulkTarget {
    /// The row's name, as its list shows it.
    pub fn name(&self) -> String {
        match self {
            Self::Bot(id) => id.to_string(),
            Self::Task { id, label, .. } if label.is_empty() => id.to_string(),
            Self::Task { label, .. } => label.clone(),
            Self::Node(id) => id.to_string(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn names_the_row() {
        assert_eq!(BulkTarget::Bot(BotId::new("worker-1").unwrap()).name(), "worker-1");
        let task = BulkTarget::Task {
            id: TaskId::new("task-1").unwrap(),
            call: Some("c1".into()),
            label: String::new(),
        };
        assert_eq!(task.name(), "task-1");
        let named = BulkTarget::Task {
            id: TaskId::new("task-1").unwrap(),
            call: None,
            label: "worker-1 · Restart it".into(),
        };
        assert_eq!(named.name(), "worker-1 · Restart it");
        assert_eq!(BulkTarget::Node(NodeId::new("edge-1").unwrap()).name(), "edge-1");
    }
}
