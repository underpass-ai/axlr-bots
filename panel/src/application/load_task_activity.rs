use std::sync::Arc;

use super::{ControlPlane, PanelError};
use crate::domain::{TaskActivity, TaskId};

/// Reads a task with everything under it: its executions, its timeline
/// and the tasks it delegated.
pub struct LoadTaskActivity {
    control_plane: Arc<dyn ControlPlane>,
}

impl LoadTaskActivity {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, id: TaskId) -> Result<TaskActivity, PanelError> {
        self.control_plane.task_activity(id).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::test_support::{fleet, run};
    use crate::domain::{TaskFilter, TaskStatus};

    #[test]
    fn reads_the_task_with_its_executions_and_timeline() {
        let (control_plane, _) = fleet();
        let tasks = run(control_plane.list_tasks(TaskFilter::default())).unwrap();
        let waiting = tasks
            .into_iter()
            .find(|t| t.status == TaskStatus::WaitingApproval)
            .unwrap();
        let tree = run(LoadTaskActivity::new(control_plane.clone()).execute(waiting.id.clone())).unwrap();
        assert_eq!(tree.task.id, waiting.id);
        assert_eq!(tree.executions.len(), 1);
        assert_eq!(tree.subtree_usage.requests, 3);
        assert!(matches!(
            run(LoadTaskActivity::new(control_plane).execute(TaskId::new("task-none").unwrap())),
            Err(PanelError::NotFound(_))
        ));
    }
}
