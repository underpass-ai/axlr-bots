use std::sync::Arc;

use super::{ControlPlane, PanelError, TaskDetail};
use crate::domain::TaskId;

/// How many of a task's events its timeline reads.
pub const TIMELINE_EVENTS: u32 = 200;

/// Reads a task, its executions and its timeline.
pub struct LoadTaskDetail {
    control_plane: Arc<dyn ControlPlane>,
}

impl LoadTaskDetail {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, id: TaskId) -> Result<TaskDetail, PanelError> {
        let (task, executions, events) = futures::try_join!(
            self.control_plane.get_task(id.clone()),
            self.control_plane.task_executions(id.clone()),
            self.control_plane.task_events(id, TIMELINE_EVENTS),
        )?;
        Ok(TaskDetail {
            task,
            executions,
            events,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::test_support::{fleet, run};
    use crate::domain::{TaskFilter, TaskStatus};

    fn waiting_task(control_plane: &crate::adapters::fake::FakeControlPlane) -> TaskId {
        let tasks = run(control_plane.list_tasks(TaskFilter::default())).unwrap();
        tasks
            .into_iter()
            .find(|t| t.status == TaskStatus::WaitingApproval)
            .unwrap()
            .id
    }

    #[test]
    fn reads_the_task_its_executions_and_its_timeline() {
        let (control_plane, _) = fleet();
        let id = waiting_task(&control_plane);
        let detail = run(LoadTaskDetail::new(control_plane.clone()).execute(id)).unwrap();
        assert_eq!(detail.executions.len(), 1);
        assert_eq!(detail.events.len(), 2);
        let pending = detail.pending_call().unwrap();
        assert_eq!(pending.tool, "local_write");
        assert_eq!(detail.total_usage().requests, 3);
        assert!(detail.answer().is_none());
        assert_eq!(detail.last_change(), detail.events[1].created_at);
    }

    #[test]
    fn a_task_that_does_not_exist_is_not_found() {
        let (control_plane, _) = fleet();
        let error = run(LoadTaskDetail::new(control_plane).execute(TaskId::new("task-none").unwrap())).unwrap_err();
        assert!(matches!(error, PanelError::NotFound(_)));
    }
}
