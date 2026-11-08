use super::{NewBotDefaults, PanelConfig, PanelError, PanelSettings};

/// Keeps the panel's settings between runs and knows the defaults of the
/// local install.
pub trait ConfigStore: Send + Sync {
    /// The saved settings; `None` on the first run.
    fn load(&self) -> Result<Option<PanelSettings>, PanelError>;
    fn save(&self, settings: &PanelSettings) -> Result<(), PanelError>;
    /// The control plane settings read from the local install, for the
    /// first-run screen.
    fn defaults(&self) -> PanelConfig;
    /// What the New bot form starts with on this host.
    fn new_bot_defaults(&self) -> NewBotDefaults {
        NewBotDefaults::default()
    }
    /// Where the settings live, in words, for the settings screen.
    fn location(&self) -> String;
}
