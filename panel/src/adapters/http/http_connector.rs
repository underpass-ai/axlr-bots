use std::sync::Arc;

use super::HttpControlPlane;
use crate::application::{Connector, ControlPlane, PanelConfig, PanelError};

/// Connects to the control plane the settings name, over HTTPS.
#[derive(Default)]
pub struct HttpConnector;

impl Connector for HttpConnector {
    fn connect(&self, config: &PanelConfig) -> Result<Arc<dyn ControlPlane>, PanelError> {
        Ok(Arc::new(HttpControlPlane::new(config)?))
    }
}
