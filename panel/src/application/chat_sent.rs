use crate::domain::{SentTaskMessage, SubmittedTask};

/// Where a chat message went: to the bot's running task, which reads it
/// between its steps, or as a new task of the bot.
#[derive(Debug, Clone, PartialEq)]
pub enum ChatSent {
    ToRun(SentTaskMessage),
    AsTask(Box<SubmittedTask>),
}
