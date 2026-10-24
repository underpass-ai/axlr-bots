use super::{PanelConfig, PanelError};

/// Keeps the panel's settings between runs and knows the defaults of the
/// local install.
pub trait ConfigStore: Send + Sync {
    /// The saved settings; `None` on the first run.
    fn load(&self) -> Result<Option<PanelConfig>, PanelError>;
    fn save(&self, config: &PanelConfig) -> Result<(), PanelError>;
    /// The settings read from the local install, for the first-run screen.
    fn defaults(&self) -> PanelConfig;
}
