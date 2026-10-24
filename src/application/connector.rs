use std::sync::Arc;

use super::{ControlPlane, PanelConfig, PanelError};

/// Builds a control plane client from settings: what the settings screen
/// tries before it saves.
pub trait Connector: Send + Sync {
    fn connect(&self, config: &PanelConfig) -> Result<Arc<dyn ControlPlane>, PanelError>;
}
