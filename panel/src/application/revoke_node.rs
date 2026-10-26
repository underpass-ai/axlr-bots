use std::sync::Arc;

use super::{ControlPlane, PanelError};
use crate::domain::{NodeId, RevokedNode};

/// Cuts a node off: its certificate is refused and what it ran fails.
pub struct RevokeNode {
    control_plane: Arc<dyn ControlPlane>,
}

impl RevokeNode {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, id: NodeId) -> Result<RevokedNode, PanelError> {
        self.control_plane.revoke_node(id).await
    }
}
