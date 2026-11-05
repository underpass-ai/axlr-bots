use std::cmp::Ordering;

use super::SortColumn;
use crate::domain::{Node, NodeStatus};

/// The Nodes table's columns a person can sort by.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NodeColumn {
    Status,
    Node,
    /// Ascending, the one seen last first.
    LastSeen,
    /// Ascending, the certificate that runs out first on top.
    Certificate,
    Axlr,
}

impl SortColumn for NodeColumn {
    type Row = Node;

    fn label(self) -> &'static str {
        match self {
            Self::Status => "Status",
            Self::Node => "Node",
            Self::LastSeen => "Last seen",
            Self::Certificate => "Certificate",
            Self::Axlr => "AXLR",
        }
    }

    fn compare(self, a: &Node, b: &Node) -> Ordering {
        match self {
            Self::Status => rank(a.status).cmp(&rank(b.status)),
            Self::Node => a.id.cmp(&b.id),
            Self::LastSeen => b.last_seen_at.cmp(&a.last_seen_at),
            // A node with no certificate (a local socket) goes last.
            Self::Certificate => match (a.certificate_not_after, b.certificate_not_after) {
                (Some(x), Some(y)) => x.cmp(&y),
                (Some(_), None) => Ordering::Less,
                (None, Some(_)) => Ordering::Greater,
                (None, None) => Ordering::Equal,
            },
            Self::Axlr => a.capabilities.axlr_version.cmp(&b.capabilities.axlr_version),
        }
    }
}

/// Online first, then the ones down, then the ones gone.
fn rank(status: NodeStatus) -> u8 {
    match status {
        NodeStatus::Online => 0,
        NodeStatus::Offline => 1,
        NodeStatus::Registered => 2,
        NodeStatus::Revoked => 3,
        NodeStatus::Unknown => 4,
    }
}

#[cfg(test)]
mod tests {
    use chrono::Duration;

    use super::*;
    use crate::application::ControlPlane;
    use crate::application::test_support::{fleet, now, run};
    use crate::ui::table::{SortDirection, SortSpec};

    fn nodes() -> Vec<Node> {
        let (cp, _) = fleet();
        let mut nodes = run(cp.list_nodes()).unwrap();
        let mut third = nodes[0].clone();
        third.id = crate::domain::NodeId::new("edge-1").unwrap();
        third.status = NodeStatus::Revoked;
        third.certificate_not_after = None;
        third.last_seen_at = now() - Duration::days(2);
        nodes.push(third);
        nodes[0].certificate_not_after = Some(now() + Duration::days(30));
        nodes[1].certificate_not_after = Some(now() + Duration::days(3));
        nodes
    }

    fn ids(spec: SortSpec<NodeColumn>, nodes: &[Node]) -> Vec<String> {
        spec.order(nodes).into_iter().map(|n| n.id.to_string()).collect()
    }

    #[test]
    fn sorts_nodes_by_status_certificate_and_sight() {
        let nodes = nodes();
        assert_eq!(
            ids(
                SortSpec::by(NodeColumn::Status, SortDirection::Ascending)
                    .then(NodeColumn::Node, SortDirection::Ascending),
                &nodes
            ),
            vec!["gx10-ff95", "wrx80", "edge-1"]
        );
        assert_eq!(
            ids(SortSpec::by(NodeColumn::Certificate, SortDirection::Ascending), &nodes),
            vec!["wrx80", "gx10-ff95", "edge-1"]
        );
        assert_eq!(
            ids(SortSpec::by(NodeColumn::Certificate, SortDirection::Descending), &nodes)[0],
            "edge-1"
        );
        assert_eq!(
            *ids(SortSpec::by(NodeColumn::LastSeen, SortDirection::Ascending), &nodes)
                .last()
                .unwrap(),
            "edge-1"
        );
        assert_eq!(
            ids(SortSpec::by(NodeColumn::Axlr, SortDirection::Ascending), &nodes).len(),
            3
        );
        let all = [
            NodeStatus::Online,
            NodeStatus::Offline,
            NodeStatus::Registered,
            NodeStatus::Revoked,
            NodeStatus::Unknown,
        ];
        assert!(all.windows(2).all(|w| rank(w[0]) < rank(w[1])));
        for column in [
            NodeColumn::Status,
            NodeColumn::Node,
            NodeColumn::LastSeen,
            NodeColumn::Certificate,
            NodeColumn::Axlr,
        ] {
            assert!(!column.label().is_empty());
        }
    }
}
