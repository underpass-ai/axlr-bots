use std::sync::Arc;

use super::{ChatTurn, ControlPlane, PanelError};
use crate::domain::{BotId, Execution, TaskFilter};

/// How many turns a bot's chat shows.
pub const CHAT_TURNS: usize = 6;

/// Reads a bot's chat: its newest tasks sent from the chat, oldest first,
/// each with the bot's answer.
pub struct LoadChat {
    control_plane: Arc<dyn ControlPlane>,
}

impl LoadChat {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, bot: BotId) -> Result<Vec<ChatTurn>, PanelError> {
        let filter = TaskFilter {
            bot: Some(bot),
            ..TaskFilter::with_limit(200)
        };
        let mut tasks: Vec<_> = self
            .control_plane
            .list_tasks(filter)
            .await?
            .into_iter()
            .filter(ChatTurn::is_chat)
            .take(CHAT_TURNS)
            .collect();
        tasks.reverse();
        let reads = tasks.iter().map(|t| self.control_plane.task_executions(t.id.clone()));
        let executions = futures::future::join_all(reads).await;
        Ok(tasks
            .into_iter()
            .zip(executions)
            .map(|(task, executions)| ChatTurn {
                task,
                answer: executions.ok().and_then(|e| newest_answer(&e)),
            })
            .collect())
    }
}

/// The newest answer an execution gave.
fn newest_answer(executions: &[Execution]) -> Option<String> {
    executions
        .iter()
        .rev()
        .map(|e| e.result.answer.trim())
        .find(|a| !a.is_empty())
        .map(str::to_string)
}
