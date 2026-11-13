use super::{Execution, ProgressCut, ProgressItem};

/// An execution with what is known of its progress: where the recorded
/// timeline was cut and the steps completed from the transcript after it.
#[derive(Debug, Clone, PartialEq)]
pub struct ExecutionActivity {
    pub execution: Execution,
    pub progress_cut: Option<ProgressCut>,
    pub completed_items: Vec<ProgressItem>,
}

impl ExecutionActivity {
    /// An execution nothing else is known of yet.
    pub fn bare(execution: Execution) -> Self {
        Self {
            execution,
            progress_cut: None,
            completed_items: Vec::new(),
        }
    }

    /// The steps after the cut were read from the transcript.
    pub fn from_transcript(&self) -> bool {
        self.progress_cut.as_ref().is_some_and(|cut| cut.from_transcript)
    }
}
