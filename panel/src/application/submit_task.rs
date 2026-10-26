use std::sync::Arc;

use super::{ControlPlane, KeySource, PanelError};
use crate::domain::{BotId, IdempotencyKey, Objective, SubmittedTask, TaskSubmission};

/// Submits a task to a bot. The form keeps one key from the moment it
/// opens, so a retry after a timeout names the same task.
pub struct SubmitTask {
    control_plane: Arc<dyn ControlPlane>,
    keys: Arc<dyn KeySource>,
}

impl SubmitTask {
    pub fn new(control_plane: Arc<dyn ControlPlane>, keys: Arc<dyn KeySource>) -> Self {
        Self { control_plane, keys }
    }

    /// A fresh key for a new form.
    pub fn new_key(&self) -> IdempotencyKey {
        self.keys.next_key()
    }

    pub async fn execute(&self, bot: &str, objective: &str, key: IdempotencyKey) -> Result<SubmittedTask, PanelError> {
        let submission = TaskSubmission {
            bot_id: BotId::new(bot)?,
            objective: Objective::new(objective)?,
            key,
        };
        self.control_plane.submit_task(submission).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::adapters::fake::CountingKeys;
    use crate::application::test_support::{fleet, run};
    use crate::application::{CancelTask, SwitchBot};
    use crate::domain::TaskStatus;

    #[test]
    fn submits_once_per_key() {
        let (control_plane, _) = fleet();
        let uc = SubmitTask::new(control_plane.clone(), Arc::new(CountingKeys::default()));
        let key = uc.new_key();
        assert_eq!(key.as_str(), "test-1");
        let first = run(uc.execute("bot-wrx80", "Count the files", key.clone())).unwrap();
        assert!(first.created);
        assert_eq!(first.task.status, TaskStatus::Pending);
        let retry = run(uc.execute("bot-wrx80", "Count the files", key)).unwrap();
        assert!(!retry.created);
        assert_eq!(retry.task.id, first.task.id);
        let other = run(uc.execute("bot-wrx80", "Count again", uc.new_key())).unwrap();
        assert_ne!(other.task.id, first.task.id);
    }

    #[test]
    fn checks_the_input_and_the_bot() {
        let (control_plane, _) = fleet();
        let uc = SubmitTask::new(control_plane.clone(), Arc::new(CountingKeys::default()));
        assert!(matches!(
            run(uc.execute("Bad Bot", "x", uc.new_key())),
            Err(PanelError::Invalid(_))
        ));
        assert!(matches!(
            run(uc.execute("bot", "  ", uc.new_key())),
            Err(PanelError::Invalid(_))
        ));
        assert!(matches!(
            run(uc.execute("ghost", "x", uc.new_key())),
            Err(PanelError::NotFound(_))
        ));
        run(SwitchBot::new(control_plane.clone()).pause(BotId::new("bot").unwrap())).unwrap();
        assert!(matches!(
            run(uc.execute("bot", "x", uc.new_key())),
            Err(PanelError::State(_))
        ));
    }

    #[test]
    fn cancels_a_task_that_has_not_ended() {
        let (control_plane, _) = fleet();
        let uc = SubmitTask::new(control_plane.clone(), Arc::new(CountingKeys::default()));
        let task = run(uc.execute("bot", "Wait a while", uc.new_key())).unwrap().task;
        let cancel = CancelTask::new(control_plane.clone());
        let cancelled = run(cancel.execute(task.id.clone())).unwrap();
        assert!(cancelled.changed);
        assert_eq!(cancelled.task.status, TaskStatus::Cancelled);
        assert!(!run(cancel.execute(task.id.clone())).unwrap().changed);
        control_plane.finish_task(&task.id, TaskStatus::Completed);
        assert!(matches!(run(cancel.execute(task.id)), Err(PanelError::State(_))));
    }
}
