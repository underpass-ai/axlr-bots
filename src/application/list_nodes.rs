use std::sync::Arc;

use super::{ControlPlane, PanelError};
use crate::domain::Node;

/// The nodes, in id order.
pub struct ListNodes {
    control_plane: Arc<dyn ControlPlane>,
}

impl ListNodes {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self) -> Result<Vec<Node>, PanelError> {
        let mut nodes = self.control_plane.list_nodes().await?;
        nodes.sort_by(|a, b| a.id.cmp(&b.id));
        Ok(nodes)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::test_support::{fleet, run};
    use crate::application::{RevokeNode, ShowNode};
    use crate::domain::{NodeId, NodeStatus};

    #[test]
    fn lists_shows_and_revokes_nodes() {
        let (control_plane, _) = fleet();
        let nodes = run(ListNodes::new(control_plane.clone()).execute()).unwrap();
        assert_eq!(
            nodes.iter().map(|n| n.id.as_str()).collect::<Vec<_>>(),
            vec!["gx10-ff95", "wrx80"]
        );
        let wrx80 = NodeId::new("wrx80").unwrap();
        let node = run(ShowNode::new(control_plane.clone()).execute(wrx80.clone())).unwrap();
        assert_eq!(node.host_name, "wrx80-node1");
        let revoked = run(RevokeNode::new(control_plane.clone()).execute(wrx80.clone())).unwrap();
        assert_eq!(revoked.node.status, NodeStatus::Revoked);
        assert!(matches!(
            run(RevokeNode::new(control_plane.clone()).execute(wrx80)),
            Err(PanelError::State(_))
        ));
        let ghost = NodeId::new("ghost").unwrap();
        assert!(matches!(
            run(ShowNode::new(control_plane.clone()).execute(ghost)),
            Err(PanelError::NotFound(_))
        ));
        control_plane.fail_with(Some(PanelError::Server("boom".into())));
        assert!(run(ListNodes::new(control_plane).execute()).is_err());
    }
}
