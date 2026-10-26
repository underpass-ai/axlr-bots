use std::sync::Arc;

use super::{ControlPlane, PanelError};
use crate::domain::{Execution, TaskId};

/// A task's executions with their results, oldest first.
pub struct ListExecutions {
    control_plane: Arc<dyn ControlPlane>,
}

impl ListExecutions {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, id: TaskId) -> Result<Vec<Execution>, PanelError> {
        let mut executions = self.control_plane.task_executions(id).await?;
        executions.sort_by_key(|e| e.started_at);
        Ok(executions)
    }
}
