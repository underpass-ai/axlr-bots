use super::PanelError;
use crate::domain::Shortcut;

/// Where the operator's one-press buttons come from: a file the operator
/// writes, read once when the panel starts.
pub trait ShortcutSource: Send + Sync {
    fn load(&self) -> Result<Vec<Shortcut>, PanelError>;
}
