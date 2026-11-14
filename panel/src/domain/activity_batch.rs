use super::ActivityUpdate;

/// What a watch of a task's activity brought: the updates that came, and
/// the cursor to resume from.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct ActivityBatch {
    pub cursor: Option<String>,
    pub updates: Vec<ActivityUpdate>,
}
