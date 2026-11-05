use std::sync::Arc;

use super::{ControlPlane, PanelError};
use crate::domain::{Task, TaskFilter};

/// The tasks a filter selects, newest first.
pub struct ListTasks {
    control_plane: Arc<dyn ControlPlane>,
}

impl ListTasks {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, filter: TaskFilter) -> Result<Vec<Task>, PanelError> {
        let mut tasks = self.control_plane.list_tasks(filter).await?;
        tasks.sort_by(|a, b| b.created_at.cmp(&a.created_at).then_with(|| b.id.cmp(&a.id)));
        Ok(tasks)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::ListExecutions;
    use crate::application::test_support::{fleet, now, run};
    use crate::domain::{BotId, TaskStatus};

    #[test]
    fn filters_and_orders_newest_first() {
        let (control_plane, _) = fleet();
        let uc = ListTasks::new(control_plane.clone());
        let all = run(uc.execute(TaskFilter::default())).unwrap();
        assert_eq!(all.len(), 5);
        assert!(all.windows(2).all(|w| w[0].created_at >= w[1].created_at));
        let mut filter = TaskFilter {
            bot: Some(BotId::new("bot").unwrap()),
            ..Default::default()
        };
        assert_eq!(run(uc.execute(filter.clone())).unwrap().len(), 3);
        filter.status = Some(TaskStatus::Failed);
        assert_eq!(run(uc.execute(filter.clone())).unwrap().len(), 1);
        filter.status = Some(TaskStatus::Expired);
        assert_eq!(run(uc.execute(filter)).unwrap().len(), 1);
        let recent = TaskFilter {
            since: Some(now() - chrono::Duration::minutes(3)),
            ..Default::default()
        };
        assert_eq!(run(uc.execute(recent)).unwrap().len(), 3);
        let limited = TaskFilter::with_limit(1);
        assert_eq!(run(uc.execute(limited)).unwrap().len(), 1);
        let ghost = TaskFilter {
            bot: Some(BotId::new("ghost").unwrap()),
            ..Default::default()
        };
        assert!(matches!(run(uc.execute(ghost)), Err(PanelError::NotFound(_))));
    }

    #[test]
    fn a_pending_task_of_a_bot_whose_node_is_away_says_what_it_waits_for() {
        let (control_plane, _) = fleet();
        control_plane.set_node_status("wrx80", crate::domain::NodeStatus::Offline);
        let submission = crate::domain::TaskSubmission {
            bot_id: BotId::new("bot-wrx80").unwrap(),
            objective: crate::domain::Objective::new("Look at the host").unwrap(),
            key: crate::domain::IdempotencyKey::new("k-away").unwrap(),
            urgent: false,
        };
        let submitted = run(control_plane.submit_task(submission)).unwrap().task;
        let tasks = run(ListTasks::new(control_plane.clone()).execute(TaskFilter::default())).unwrap();
        let waiting = tasks.iter().find(|t| t.id == submitted.id).unwrap();
        assert_eq!(waiting.waiting, "waiting for node wrx80, which is Offline");
        assert!(
            tasks
                .iter()
                .filter(|t| t.id != submitted.id)
                .all(|t| t.waiting.is_empty())
        );
        let read = run(control_plane.get_task(submitted.id.clone())).unwrap();
        assert_eq!(
            read.waits_for().as_deref(),
            Some("Waiting for node wrx80, which is Offline")
        );
        control_plane.set_node_status("wrx80", crate::domain::NodeStatus::Online);
        assert!(run(control_plane.get_task(submitted.id)).unwrap().waiting.is_empty());
    }

    #[test]
    fn lists_a_tasks_executions() {
        let (control_plane, _) = fleet();
        let tasks = run(ListTasks::new(control_plane.clone()).execute(TaskFilter::default())).unwrap();
        let done = tasks.iter().find(|t| t.status == TaskStatus::Completed).unwrap();
        let executions = run(ListExecutions::new(control_plane.clone()).execute(done.id.clone())).unwrap();
        assert_eq!(executions.len(), 1);
        assert_eq!(executions[0].place(), "wrx80");
    }
}
