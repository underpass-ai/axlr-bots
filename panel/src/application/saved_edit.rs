use crate::domain::Bot;

/// How a save of a bot's edit ended.
#[derive(Debug, Clone, PartialEq)]
pub enum SavedEdit {
    /// The control plane has the edit; the bot as it now is.
    Saved(Bot),
    /// Nothing to change: the bot already looks like the edit.
    Unchanged(Bot),
    /// Somebody changed the bot since the edit opened; nothing was
    /// written. The bot as it now is, to check the edit against.
    ChangedMeanwhile(Bot),
}
