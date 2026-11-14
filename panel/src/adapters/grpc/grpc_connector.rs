use std::sync::Arc;

use super::GrpcControlPlane;
use crate::application::{Connector, ControlPlane, PanelConfig, PanelError};

/// Connects to the control plane the settings name, over gRPC with TLS.
#[derive(Default)]
pub struct GrpcConnector;

impl Connector for GrpcConnector {
    fn connect(&self, config: &PanelConfig) -> Result<Arc<dyn ControlPlane>, PanelError> {
        Ok(Arc::new(GrpcControlPlane::new(config)?))
    }
}
