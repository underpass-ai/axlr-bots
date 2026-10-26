use crate::domain::{Task, TaskStatus};

/// What starts the key of a task sent from a bot's chat.
pub const CHAT_KEY_PREFIX: &str = "chat-";

/// What ends the person's words in a chat task's objective; the earlier
/// turns follow it, for the bot's context.
pub const CHAT_CONTEXT_MARK: &str = "\n\n---\nEarlier in this chat";

/// One message to a bot from its chat, and what the bot answered: a task
/// sent with a `chat-` key, and its newest answer.
#[derive(Debug, Clone, PartialEq)]
pub struct ChatTurn {
    pub task: Task,
    pub answer: Option<String>,
}

impl ChatTurn {
    /// Whether a task came from a bot's chat.
    pub fn is_chat(task: &Task) -> bool {
        task.idempotency_key.starts_with(CHAT_KEY_PREFIX)
    }

    /// The person's words, without the earlier turns sent as context.
    pub fn message(&self) -> &str {
        let objective = self.task.objective.as_str();
        objective.split(CHAT_CONTEXT_MARK).next().unwrap_or(objective).trim()
    }

    /// The bot's side of the turn: its answer, or where the task stands.
    pub fn reply(&self) -> String {
        if let Some(answer) = self.answer.as_deref().filter(|a| !a.trim().is_empty()) {
            return answer.trim().to_string();
        }
        match self.task.status {
            TaskStatus::Pending => "Got it. It starts as soon as a node is free.".into(),
            TaskStatus::Running => "Working on it…".into(),
            TaskStatus::WaitingApproval => "It waits for your OK before it goes on.".into(),
            TaskStatus::Completed => "Done, with nothing to say.".into(),
            TaskStatus::Failed if !self.task.failure_reason.is_empty() => {
                format!("It failed: {}", self.task.failure_reason)
            }
            TaskStatus::Expired => "Nobody answered its question in time.".into(),
            TaskStatus::Cancelled => "Stopped.".into(),
            _ => self.task.status.label().to_string(),
        }
    }
}
