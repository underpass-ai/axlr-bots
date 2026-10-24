use serde::{Deserialize, Serialize};

/// What a node's AXLR can do: its version and whether it takes a policy
/// file and the correlation ids.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct NodeCapabilities {
    #[serde(default, rename = "axlrVersion")]
    pub axlr_version: String,
    #[serde(default)]
    pub policy_supported: bool,
    #[serde(default)]
    pub correlation_supported: bool,
}
