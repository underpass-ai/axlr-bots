use super::Place;
use crate::domain::{BotId, NodeId, TaskId, TaskStatus};

/// What choosing a result of the command palette does. The palette only
/// names it; the panel carries it out.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PaletteTarget {
    /// Go to a place.
    Go(Place),
    /// Open the new task's form.
    NewTask,
    /// Open the new bot's form.
    NewBot,
    /// Light to dark, or dark to light.
    SwitchTheme,
    /// The task list with only the tasks that stand so: the ones that
    /// need a person, or the ones that failed.
    ShowTasks(TaskStatus),
    /// Open a bot beside the bot list.
    Bot(BotId),
    /// Open a team, by its name.
    Team(String),
    /// Open a node beside the node list. A revoked node is hidden there
    /// until asked for: the list has to show them first.
    Node { id: NodeId, revoked: bool },
    /// Open a task beside the task list.
    Task(TaskId),
}
