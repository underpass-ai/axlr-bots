use serde::{Deserialize, Serialize};

use super::ToolRule;

/// The calls a bot's policy lets through and the ones it refuses.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct ToolRules {
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub allow: Vec<ToolRule>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub deny: Vec<ToolRule>,
}

impl ToolRules {
    pub fn is_empty(&self) -> bool {
        self.allow.is_empty() && self.deny.is_empty()
    }
}
