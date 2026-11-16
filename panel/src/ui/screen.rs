use super::table::{List, RowRef};
use crate::domain::{BotId, NodeId, TaskId};

/// Where the operator is. A row's own place is its list with the row
/// open in the inspector beside it: `Task(id)` is the Tasks list with
/// that task inspected.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub enum Screen {
    #[default]
    Overview,
    Nodes,
    Node(NodeId),
    Bots,
    Bot(BotId),
    Tasks,
    Task(TaskId),
    Teams,
    Team(String),
    Settings,
}

/// The rail's places; a detail belongs to its list's place.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Section {
    Overview,
    Nodes,
    Bots,
    Teams,
    Tasks,
    Settings,
}

impl Screen {
    /// `overview`, `nodes`, `node:<id>`, `bots`, `bot:<id>`, `tasks`,
    /// `task:<id>`, `teams`, `team:<name>`, `settings`: what `--screen`
    /// takes. A row opens its list with the row inspected.
    pub fn parse(raw: &str) -> Option<Self> {
        match raw.split_once(':') {
            Some(("bot", id)) => BotId::new(id).ok().map(Self::Bot),
            Some(("task", id)) => TaskId::new(id).ok().map(Self::Task),
            Some(("node", id)) => NodeId::new(id).ok().map(Self::Node),
            Some(("team", name)) if !name.is_empty() => Some(Self::Team(name.to_string())),
            Some(_) => None,
            None => match raw {
                "overview" => Some(Self::Overview),
                "nodes" => Some(Self::Nodes),
                "bots" => Some(Self::Bots),
                "tasks" => Some(Self::Tasks),
                "teams" => Some(Self::Teams),
                "settings" => Some(Self::Settings),
                _ => None,
            },
        }
    }

    pub fn section(&self) -> Section {
        match self {
            Self::Overview => Section::Overview,
            Self::Nodes | Self::Node(_) => Section::Nodes,
            Self::Bots | Self::Bot(_) => Section::Bots,
            Self::Tasks | Self::Task(_) => Section::Tasks,
            Self::Teams | Self::Team(_) => Section::Teams,
            Self::Settings => Section::Settings,
        }
    }

    /// The list on show, with a row inspected or not.
    pub fn list(&self) -> Option<List> {
        match self {
            Self::Nodes | Self::Node(_) => Some(List::Nodes),
            Self::Bots | Self::Bot(_) => Some(List::Bots),
            Self::Tasks | Self::Task(_) => Some(List::Tasks),
            Self::Teams | Self::Team(_) => Some(List::Teams),
            Self::Overview | Self::Settings => None,
        }
    }

    /// The row the inspector shows, if one is open.
    pub fn inspected(&self) -> Option<RowRef> {
        match self {
            Self::Node(id) => Some(RowRef::Node(id.clone())),
            Self::Bot(id) => Some(RowRef::Bot(id.clone())),
            Self::Task(id) => Some(RowRef::Task(id.clone())),
            Self::Team(name) => Some(RowRef::Team(name.clone())),
            _ => None,
        }
    }

    /// The list with the row inspected.
    pub fn of_row(row: RowRef) -> Self {
        match row {
            RowRef::Node(id) => Self::Node(id),
            RowRef::Bot(id) => Self::Bot(id),
            RowRef::Task(id) => Self::Task(id),
            RowRef::Team(name) => Self::Team(name),
        }
    }

    /// The list alone, the inspector closed.
    pub fn of_list(list: List) -> Self {
        match list {
            List::Nodes => Self::Nodes,
            List::Bots => Self::Bots,
            List::Tasks => Self::Tasks,
            List::Teams => Self::Teams,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_the_screen_flag() {
        assert_eq!(Screen::parse("nodes"), Some(Screen::Nodes));
        assert_eq!(Screen::parse("overview"), Some(Screen::Overview));
        assert_eq!(Screen::parse("bots"), Some(Screen::Bots));
        assert_eq!(Screen::parse("tasks"), Some(Screen::Tasks));
        assert_eq!(Screen::parse("settings"), Some(Screen::Settings));
        assert_eq!(
            Screen::parse("bot:bot-wrx80"),
            Some(Screen::Bot(BotId::new("bot-wrx80").unwrap()))
        );
        assert_eq!(
            Screen::parse("task:task-1"),
            Some(Screen::Task(TaskId::new("task-1").unwrap()))
        );
        assert_eq!(
            Screen::parse("activity:task-1"),
            None,
            "the activity is a tab of its task"
        );
        assert_eq!(Screen::parse("task:Bad Id"), None);
        assert_eq!(
            Screen::parse("node:node-03"),
            Some(Screen::Node(NodeId::new("node-03").unwrap()))
        );
        assert_eq!(Screen::parse("node:Bad Id"), None);
        assert_eq!(Screen::parse("home"), None);
        assert_eq!(Screen::parse("teams"), Some(Screen::Teams));
        assert_eq!(Screen::parse("team:ops"), Some(Screen::Team("ops".into())));
        assert_eq!(Screen::parse("team:"), None);
        assert_eq!(Screen::Team("ops".into()).section(), Section::Teams);
        assert_eq!(Screen::Task(TaskId::new("t").unwrap()).section(), Section::Tasks);
        assert_eq!(Screen::Bot(BotId::new("b").unwrap()).section(), Section::Bots);
        assert_eq!(Screen::default().section(), Section::Overview);
        assert_eq!(Screen::Nodes.section(), Section::Nodes);
        assert_eq!(Screen::Settings.section(), Section::Settings);
        assert_eq!(Screen::parse("incidents"), None);
    }

    #[test]
    fn a_rows_place_is_its_list_with_the_row_inspected() {
        let task = TaskId::new("task-1").unwrap();
        let node = NodeId::new("node-03").unwrap();
        assert_eq!(Screen::Task(task.clone()).list(), Some(List::Tasks));
        assert_eq!(Screen::Node(node.clone()).section(), Section::Nodes);
        assert_eq!(Screen::Team("ops".into()).list(), Some(List::Teams));
        assert_eq!(Screen::Overview.list(), None);
        assert_eq!(Screen::Bots.inspected(), None);
        for row in [
            RowRef::Task(task),
            RowRef::Node(node),
            RowRef::Bot(BotId::new("b").unwrap()),
            RowRef::Team("ops".into()),
        ] {
            let screen = Screen::of_row(row.clone());
            assert_eq!(screen.inspected(), Some(row));
            assert_eq!(Screen::of_list(screen.list().unwrap()).list(), screen.list());
        }
    }
}
