use crate::domain::{Node, NodeStatus};

/// Which nodes the node list shows and in what order: the revoked ones
/// stay hidden until a person asks for them, and the nodes that answer
/// come first.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct NodeRows {
    revoked: bool,
}

impl NodeRows {
    /// The revoked nodes shown too.
    pub fn with_revoked() -> Self {
        Self { revoked: true }
    }

    pub fn shows_revoked(self) -> bool {
        self.revoked
    }

    /// The Revoked chip or `Show them`: shows the revoked nodes, or
    /// hides them again.
    pub fn toggle(&mut self) {
        self.revoked = !self.revoked;
    }

    /// Shows the revoked nodes (`true`), or hides them.
    pub fn set(&mut self, revoked: bool) {
        self.revoked = revoked;
    }

    /// The nodes on show: online first, then the ones not heard yet, the
    /// silent ones and, when asked for, the revoked ones; by id inside
    /// each.
    pub fn rows<'a>(self, nodes: impl IntoIterator<Item = &'a Node>) -> Vec<&'a Node> {
        let mut rows: Vec<&Node> = nodes
            .into_iter()
            .filter(|node| self.revoked || node.status != NodeStatus::Revoked)
            .collect();
        rows.sort_by(|a, b| rank(a.status).cmp(&rank(b.status)).then_with(|| a.id.cmp(&b.id)));
        rows
    }

    /// How many nodes are revoked: the number on the Revoked chip.
    pub fn revoked<'a>(nodes: impl IntoIterator<Item = &'a Node>) -> usize {
        nodes
            .into_iter()
            .filter(|node| node.status == NodeStatus::Revoked)
            .count()
    }

    /// The line that says what is not shown, `40 revoked nodes are
    /// hidden`; none when they are shown or there are none.
    pub fn note<'a>(self, nodes: impl IntoIterator<Item = &'a Node>) -> Option<String> {
        match Self::revoked(nodes) {
            _ if self.revoked => None,
            0 => None,
            1 => Some("1 revoked node is hidden".to_string()),
            hidden => Some(format!("{hidden} revoked nodes are hidden")),
        }
    }
}

/// The order of the list: what answers first, what is cut off last.
fn rank(status: NodeStatus) -> u8 {
    match status {
        NodeStatus::Online => 0,
        NodeStatus::Registered => 1,
        NodeStatus::Offline => 2,
        NodeStatus::Unknown => 3,
        NodeStatus::Revoked => 4,
    }
}

#[cfg(test)]
mod tests {
    use super::super::test_rows::node;
    use super::*;

    fn nodes() -> Vec<Node> {
        vec![
            node("node-old", "rack-1", "Revoked"),
            node("node-b", "rack-2", "Offline"),
            node("node-c", "rack-3", "Online"),
            node("node-new", "rack-4", "Registered"),
            node("node-a", "rack-5", "Online"),
            node("node-odd", "rack-6", "SomethingNew"),
            node("node-gone", "rack-7", "Revoked"),
        ]
    }

    fn ids(rows: &[&Node]) -> Vec<String> {
        rows.iter().map(|n| n.id.to_string()).collect()
    }

    #[test]
    fn hides_the_revoked_and_puts_online_first() {
        let nodes = nodes();
        let rows = NodeRows::default();
        assert!(!rows.shows_revoked());
        assert_eq!(
            ids(&rows.rows(&nodes)),
            vec!["node-a", "node-c", "node-new", "node-b", "node-odd"]
        );
        assert_eq!(rows.note(&nodes).as_deref(), Some("2 revoked nodes are hidden"));
        assert_eq!(NodeRows::revoked(&nodes), 2);
    }

    #[test]
    fn shows_the_revoked_last_when_asked() {
        let nodes = nodes();
        let mut rows = NodeRows::default();
        rows.toggle();
        assert_eq!(rows, NodeRows::with_revoked());
        assert_eq!(
            ids(&rows.rows(&nodes)),
            vec![
                "node-a",
                "node-c",
                "node-new",
                "node-b",
                "node-odd",
                "node-gone",
                "node-old"
            ]
        );
        assert_eq!(rows.note(&nodes), None);
        rows.toggle();
        assert!(!rows.shows_revoked());
        rows.set(true);
        assert_eq!(rows, NodeRows::with_revoked());
        rows.set(false);
        assert_eq!(rows, NodeRows::default());
    }

    #[test]
    fn says_nothing_when_nothing_is_hidden() {
        let live = [node("node-a", "rack-1", "Online")];
        assert_eq!(NodeRows::default().note(&live), None);
        let one = [
            node("node-a", "rack-1", "Online"),
            node("node-old", "rack-2", "Revoked"),
        ];
        assert_eq!(
            NodeRows::default().note(&one).as_deref(),
            Some("1 revoked node is hidden")
        );
        let none: [Node; 0] = [];
        assert!(NodeRows::default().rows(&none).is_empty());
        assert_eq!(NodeRows::revoked(&none), 0);
    }

    #[test]
    fn orders_what_a_filter_left() {
        let nodes = nodes();
        let kept = super::super::Query::new("rack").filter(&nodes);
        assert_eq!(NodeRows::default().rows(kept).len(), 5);
        let one = super::super::Query::new("rack-1").filter(&nodes);
        assert!(
            NodeRows::default().rows(one.clone()).is_empty(),
            "the one that answers is revoked"
        );
        assert_eq!(ids(&NodeRows::with_revoked().rows(one)), vec!["node-old"]);
    }
}
