use std::sync::Arc;

use crate::application::{Connector, ControlPlane, PanelConfig, PanelError};

/// Hands out the same control plane for any settings whose address is the
/// one it knows; any other address is unreachable.
pub struct FakeConnector {
    url: String,
    control_plane: Arc<dyn ControlPlane>,
}

impl FakeConnector {
    pub fn new(url: &str, control_plane: Arc<dyn ControlPlane>) -> Self {
        Self {
            url: url.to_string(),
            control_plane,
        }
    }
}

impl Connector for FakeConnector {
    fn connect(&self, config: &PanelConfig) -> Result<Arc<dyn ControlPlane>, PanelError> {
        if config.base_url() == self.url {
            Ok(self.control_plane.clone())
        } else {
            Err(PanelError::Unreachable(format!(
                "{}: connection refused",
                config.base_url()
            )))
        }
    }
}
