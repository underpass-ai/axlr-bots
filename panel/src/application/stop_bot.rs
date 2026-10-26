use std::sync::Arc;

use super::{ControlPlane, PanelError, StoppedBot};
use crate::domain::{BotId, TaskFilter};

/// Stops a bot at once: pauses it so it takes no new task, then cancels
/// every task of it still pending, running or waiting for a person.
pub struct StopBot {
    control_plane: Arc<dyn ControlPlane>,
}

impl StopBot {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, id: BotId) -> Result<StoppedBot, PanelError> {
        let bot = self.control_plane.get_bot(id.clone()).await?;
        let bot = if bot.status.can_pause() {
            self.control_plane.pause_bot(id.clone()).await?
        } else {
            bot
        };
        let filter = TaskFilter {
            bot: Some(id),
            ..TaskFilter::with_limit(TaskFilter::MAX_LIMIT)
        };
        let open: Vec<_> = self
            .control_plane
            .list_tasks(filter)
            .await?
            .into_iter()
            .filter(|t| t.status.can_cancel())
            .collect();
        let mut stopped = StoppedBot {
            bot,
            cancelled: 0,
            not_cancelled: 0,
        };
        for task in open {
            match self.control_plane.cancel_task(task.id).await {
                Ok(_) => stopped.cancelled += 1,
                Err(_) => stopped.not_cancelled += 1,
            }
        }
        Ok(stopped)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::test_support::{fleet, run};
    use crate::domain::{BotStatus, TaskStatus};

    #[test]
    fn pauses_the_bot_and_cancels_its_open_tasks() {
        let (control_plane, _) = fleet();
        let tasks = run(control_plane.list_tasks(TaskFilter::default())).unwrap();
        let waiting = tasks.iter().find(|t| t.status == TaskStatus::WaitingApproval).unwrap();
        let id = waiting.bot_id.clone();
        let open = tasks.iter().filter(|t| t.bot_id == id && t.status.can_cancel()).count() as u32;
        let stopped = run(StopBot::new(control_plane.clone()).execute(id.clone())).unwrap();
        assert_eq!(stopped.bot.status, BotStatus::Paused);
        assert_eq!((stopped.cancelled, stopped.not_cancelled), (open, 0));
        assert!(open >= 1);
        let after = run(control_plane.list_tasks(TaskFilter::default())).unwrap();
        assert!(after.iter().filter(|t| t.bot_id == id).all(|t| !t.status.can_cancel()));

        // A second stop finds the bot paused and nothing open.
        let again = run(StopBot::new(control_plane.clone()).execute(id)).unwrap();
        assert_eq!((again.bot.status, again.cancelled), (BotStatus::Paused, 0));
    }

    #[test]
    fn an_unknown_bot_is_not_found() {
        let (control_plane, _) = fleet();
        let error = run(StopBot::new(control_plane).execute(BotId::new("ghost").unwrap())).unwrap_err();
        assert!(matches!(error, PanelError::NotFound(_)));
    }
}
