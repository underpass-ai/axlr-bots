use serde::Deserialize;

use super::{Node, TaskId};

/// The answer to a revocation: the node and the tasks it was running,
/// which failed.
#[derive(Debug, Clone, PartialEq, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RevokedNode {
    pub node: Node,
    #[serde(default)]
    pub failed_tasks: Vec<TaskId>,
}
