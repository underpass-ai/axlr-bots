use super::ToolAccess;

/// One tool of a bot and what it may do with it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ToolSetting {
    pub tool: String,
    pub access: ToolAccess,
}
