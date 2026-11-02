use std::sync::Arc;

use super::{ControlPlane, PanelError};
use crate::domain::{DecidedTask, Decision, TaskId};

/// Approves or denies the call a waiting task stopped on.
pub struct DecideTask {
    control_plane: Arc<dyn ControlPlane>,
}

impl DecideTask {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(
        &self,
        id: TaskId,
        decision: Decision,
        call_id: Option<String>,
    ) -> Result<DecidedTask, PanelError> {
        self.control_plane.decide_task(id, decision, call_id).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::test_support::{fleet, run};
    use crate::domain::{ExecutionKind, TaskFilter, TaskStatus};

    #[test]
    fn approves_a_waiting_task_once() {
        let (control_plane, _) = fleet();
        let tasks = run(control_plane.list_tasks(TaskFilter::default())).unwrap();
        let id = tasks
            .into_iter()
            .find(|t| t.status == TaskStatus::WaitingApproval)
            .unwrap()
            .id;
        let uc = DecideTask::new(control_plane.clone());
        let decided = run(uc.execute(id.clone(), Decision::Approve, Some("call-1".into()))).unwrap();
        assert_eq!(decided.task.status, TaskStatus::Running);
        assert_eq!(decided.execution.kind, ExecutionKind::Decide);
        assert_eq!(decided.execution.decision, "approve");
        assert!(matches!(
            run(uc.execute(id.clone(), Decision::Deny, None)),
            Err(PanelError::State(_))
        ));
        assert!(control_plane.calls().contains(&format!("decide_task {id} approve")));
    }

    #[test]
    fn refuses_a_decision_on_another_call() {
        let (control_plane, _) = fleet();
        let tasks = run(control_plane.list_tasks(TaskFilter::default())).unwrap();
        let task = tasks
            .into_iter()
            .find(|t| t.status == TaskStatus::WaitingApproval)
            .unwrap();
        let uc = DecideTask::new(control_plane.clone());
        assert!(matches!(
            run(uc.execute(task.id.clone(), Decision::Approve, Some("call-0".into()))),
            Err(PanelError::State(_))
        ));
        let still = run(control_plane.get_task(task.id)).unwrap();
        assert_eq!(still.status, TaskStatus::WaitingApproval);
    }
}
