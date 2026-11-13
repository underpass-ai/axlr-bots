use super::ToolActivity;
use crate::domain::{Execution, ProgressCut};

/// One execution of a task as the activity screen shows it: how long it
/// took and what it cost (the execution holds both), the tools it called
/// with their state and duration, and whether its steps after the cut
/// were completed from the transcript.
#[derive(Debug, Clone, PartialEq)]
pub struct ActivityRun {
    pub execution: Execution,
    pub tools: Vec<ToolActivity>,
    pub cut: Option<ProgressCut>,
    pub from_transcript: bool,
}
