use std::sync::Arc;

use super::{ControlPlane, PanelError};
use crate::domain::{CancelledTask, TaskId};

/// Stops a task that has not finished.
pub struct CancelTask {
    control_plane: Arc<dyn ControlPlane>,
}

impl CancelTask {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, id: TaskId) -> Result<CancelledTask, PanelError> {
        self.control_plane.cancel_task(id).await
    }
}
