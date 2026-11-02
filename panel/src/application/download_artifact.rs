use std::path::PathBuf;
use std::sync::Arc;

use super::{ControlPlane, FilePicker, PanelError};
use crate::domain::ExecutionId;

/// Fetches an artifact the control plane keeps and saves it where the
/// operator says. The bytes come over the operator API with its token
/// and CA, which a browser has neither of; `None` when they cancel.
pub struct DownloadArtifact {
    control_plane: Arc<dyn ControlPlane>,
    picker: Arc<dyn FilePicker>,
}

impl DownloadArtifact {
    pub fn new(control_plane: Arc<dyn ControlPlane>, picker: Arc<dyn FilePicker>) -> Self {
        Self { control_plane, picker }
    }

    pub async fn execute(&self, execution: ExecutionId, name: String) -> Result<Option<PathBuf>, PanelError> {
        let bytes = self.control_plane.artifact(execution, name.clone()).await?;
        self.picker.save_as(name, bytes).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::adapters::fake::MemoryFiles;
    use crate::application::test_support::{fleet, run};
    use crate::domain::{TaskFilter, TaskStatus};

    #[test]
    fn saves_a_kept_artifact_and_refuses_one_not_kept() {
        let (control_plane, _) = fleet();
        let tasks = run(control_plane.list_tasks(TaskFilter::default())).unwrap();
        let done = tasks.iter().find(|t| t.status == TaskStatus::Completed).unwrap();
        let run_id = run(control_plane.task_executions(done.id.clone())).unwrap()[0]
            .id
            .clone();
        let files = Arc::new(MemoryFiles::default());
        let download = DownloadArtifact::new(control_plane, files.clone());
        let path = run(download.execute(run_id.clone(), "files.md".into()))
            .unwrap()
            .unwrap();
        assert_eq!(path, PathBuf::from("/saved/files.md"));
        assert!(files.saved()[&path].starts_with(b"# Files"));
        assert!(matches!(
            run(download.execute(run_id.clone(), "workspace.tar".into())),
            Err(PanelError::NotFound(_))
        ));
        files.cancel_saves();
        assert_eq!(run(download.execute(run_id, "files.md".into())).unwrap(), None);
    }
}
