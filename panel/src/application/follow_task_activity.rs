use std::sync::Arc;

use super::{ControlPlane, PanelError};
use crate::domain::{ActivityBatch, TaskId};

/// How long a follow of a task's activity waits for the next update.
pub const ACTIVITY_WAIT_SECONDS: u32 = 20;

/// Follows a task's activity tree: what changed after the cursor, until
/// the next update or the wait ends.
pub struct FollowTaskActivity {
    control_plane: Arc<dyn ControlPlane>,
}

impl FollowTaskActivity {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(
        &self,
        id: TaskId,
        cursor: Option<String>,
        wait_seconds: u32,
    ) -> Result<ActivityBatch, PanelError> {
        self.control_plane.watch_task_activity(id, cursor, wait_seconds).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::test_support::{fleet, run};
    use crate::domain::{ActivityUpdate, TaskFilter};

    #[test]
    fn starts_with_a_snapshot_and_resumes_with_updates() {
        let (control_plane, _) = fleet();
        let id = run(control_plane.list_tasks(TaskFilter::default()))
            .unwrap()
            .remove(0)
            .id;
        let uc = FollowTaskActivity::new(control_plane);
        let first = run(uc.execute(id.clone(), None, 0)).unwrap();
        assert!(matches!(first.updates[0], ActivityUpdate::Snapshot(_)));
        let next = run(uc.execute(id, first.cursor.clone(), ACTIVITY_WAIT_SECONDS)).unwrap();
        assert!(next.updates.is_empty());
        assert_eq!(next.cursor, first.cursor);
    }
}
