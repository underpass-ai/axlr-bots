use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};

use super::NodeId;

/// Where a bot's tasks run: named nodes, nodes whose labels match, or any.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct Placement {
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub nodes: Vec<NodeId>,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub selector: BTreeMap<String, String>,
}

impl Placement {
    /// In a few words: `any node`, `wrx80`, `zone=eu`.
    pub fn describe(&self) -> String {
        let mut parts: Vec<String> = self.nodes.iter().map(|n| n.to_string()).collect();
        parts.extend(self.selector.iter().map(|(k, v)| format!("{k}={v}")));
        if parts.is_empty() {
            "any node".to_string()
        } else {
            parts.join(", ")
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn describes_where_tasks_run() {
        assert_eq!(Placement::default().describe(), "any node");
        let placement: Placement = serde_json::from_str(r#"{"nodes":["wrx80"],"selector":{"zone":"eu"}}"#).unwrap();
        assert_eq!(placement.describe(), "wrx80, zone=eu");
        assert_eq!(serde_json::to_string(&Placement::default()).unwrap(), "{}");
    }
}
