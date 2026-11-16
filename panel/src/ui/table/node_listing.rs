use crate::domain::{Node, NodeStatus};

/// What the Nodes list shows of the fleet's nodes: the revoked ones,
/// which never come back, stay out of sight until asked for.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct NodeListing {
    /// The revoked nodes are listed too.
    pub revoked: bool,
}

impl NodeListing {
    /// The nodes whose rows are on show, in the order they came.
    pub fn shown<'a>(&self, nodes: impl IntoIterator<Item = &'a Node>) -> Vec<&'a Node> {
        nodes
            .into_iter()
            .filter(|n| self.revoked || n.status != NodeStatus::Revoked)
            .collect()
    }

    /// How many of the nodes are revoked, shown or not.
    pub fn revoked_among(nodes: &[Node]) -> usize {
        nodes.iter().filter(|n| n.status == NodeStatus::Revoked).count()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::ControlPlane;
    use crate::application::test_support::{fleet, run};

    #[test]
    fn revoked_nodes_stay_out_until_asked_for() {
        let (cp, _) = fleet();
        let mut nodes = run(cp.list_nodes()).unwrap();
        let all = nodes.len();
        nodes[0].status = NodeStatus::Revoked;
        let mut listing = NodeListing::default();
        assert_eq!(listing.shown(&nodes).len(), all - 1);
        assert_eq!(NodeListing::revoked_among(&nodes), 1);
        listing.revoked = true;
        assert_eq!(listing.shown(&nodes).len(), all);
    }
}
