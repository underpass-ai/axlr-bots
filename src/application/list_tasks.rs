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
        assert_eq!(all.len(), 4);
        assert!(all.windows(2).all(|w| w[0].created_at >= w[1].created_at));
        let mut filter = TaskFilter {
            bot: Some(BotId::new("bot").unwrap()),
            ..Default::default()
        };
        assert_eq!(run(uc.execute(filter.clone())).unwrap().len(), 2);
        filter.status = Some(TaskStatus::Failed);
        assert_eq!(run(uc.execute(filter)).unwrap().len(), 1);
        let recent = TaskFilter {
            since: Some(now() - chrono::Duration::minutes(3)),
            ..Default::default()
        };
        assert_eq!(run(uc.execute(recent)).unwrap().len(), 2);
        let limited = TaskFilter::with_limit(1);
        assert_eq!(run(uc.execute(limited)).unwrap().len(), 1);
        let ghost = TaskFilter {
            bot: Some(BotId::new("ghost").unwrap()),
            ..Default::default()
        };
        assert!(matches!(run(uc.execute(ghost)), Err(PanelError::NotFound(_))));
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
