use serde::{Deserialize, Serialize};

use super::ToolRule;

/// The part of a bot's approvals the panel reads: the calls the control
/// plane approves without a person. The rest (the wait, the expiry, who
/// is told and their secrets) the panel never writes back.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct BotApprovals {
    #[serde(default)]
    pub auto_approve: Vec<ToolRule>,
}
