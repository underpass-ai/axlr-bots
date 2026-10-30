use crate::application::{PanelError, ShortcutSource};
use crate::domain::Shortcut;

/// The buttons a test gives.
#[derive(Default)]
pub struct FixedShortcuts(pub Vec<Shortcut>);

impl ShortcutSource for FixedShortcuts {
    fn load(&self) -> Result<Vec<Shortcut>, PanelError> {
        Ok(self.0.clone())
    }
}
