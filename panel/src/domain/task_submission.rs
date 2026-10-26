use super::{BotId, IdempotencyKey, Objective};

/// A task to submit to a bot, with the key that keeps a retry from
/// creating a second one.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TaskSubmission {
    pub bot_id: BotId,
    pub objective: Objective,
    pub key: IdempotencyKey,
    /// Goes first and cancels the bot's work in progress.
    pub urgent: bool,
}
