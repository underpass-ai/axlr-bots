use crate::domain::{Execution, ExecutionOutcome, PendingDecision, TaskId};

/// The call a waiting task stopped on, read with the fleet so the operator
/// sees what they approve wherever the task is listed.
#[derive(Debug, Clone, PartialEq)]
pub struct WaitingCall {
    pub task: TaskId,
    pub call: PendingDecision,
}

impl WaitingCall {
    /// The newest execution's call that ended needing a decision.
    pub fn newest(executions: &[Execution]) -> Option<&PendingDecision> {
        executions
            .iter()
            .rev()
            .find(|e| e.outcome == ExecutionOutcome::DecisionRequired)
            .and_then(|e| e.result.pending_decision.as_ref())
    }
}
