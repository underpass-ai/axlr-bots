use std::sync::Arc;

use super::{ControlPlane, PanelError};
use crate::domain::{Node, NodeId};

/// One node.
pub struct ShowNode {
    control_plane: Arc<dyn ControlPlane>,
}

impl ShowNode {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, id: NodeId) -> Result<Node, PanelError> {
        self.control_plane.get_node(id).await
    }
}
