use super::List;
use crate::domain::{BotId, NodeId, TaskId};

/// A row's box ticked or not: which list, which row.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum RowRef {
    Node(NodeId),
    Bot(BotId),
    Task(TaskId),
    Team(String),
}

impl RowRef {
    pub fn list(&self) -> List {
        match self {
            Self::Node(_) => List::Nodes,
            Self::Bot(_) => List::Bots,
            Self::Task(_) => List::Tasks,
            Self::Team(_) => List::Teams,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn belongs_to_its_list() {
        assert_eq!(RowRef::Bot(BotId::new("b").unwrap()).list(), List::Bots);
        assert_eq!(RowRef::Task(TaskId::new("t").unwrap()).list(), List::Tasks);
        assert_eq!(RowRef::Node(NodeId::new("n").unwrap()).list(), List::Nodes);
        assert_eq!(RowRef::Team("ops".into()).list(), List::Teams);
    }
}
