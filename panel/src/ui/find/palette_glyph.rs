use crate::domain::{BotStatus, NodeStatus, TaskStatus};

/// What a result of the command palette is, for the small mark before
/// its words: the view picks the icon and its colour. A bot, a node and
/// a task carry where they stand, since their mark is their status'.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PaletteGlyph {
    /// Going to a place.
    Go,
    /// Making something new.
    New,
    Theme,
    /// The tasks that wait for a person.
    NeedsYou,
    /// The tasks that failed.
    Failed,
    Bot(BotStatus),
    Team,
    Node(NodeStatus),
    Task(TaskStatus),
}
