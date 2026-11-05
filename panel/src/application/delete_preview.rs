use crate::domain::BotReference;

/// What deleting a bot will do, read before the operator confirms: how
/// many of its tasks will be cancelled, and the bots that still name it.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct DeletePreview {
    pub open_tasks: u32,
    pub named_by: Vec<BotReference>,
}
