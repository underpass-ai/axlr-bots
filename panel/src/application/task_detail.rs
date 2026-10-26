use chrono::{DateTime, Utc};

use crate::domain::{Event, Execution, ExecutionOutcome, PendingDecision, Task, TaskStatus, Usage};

/// One task with everything about it: its executions and its timeline.
#[derive(Debug, Clone, PartialEq)]
pub struct TaskDetail {
    pub task: Task,
    pub executions: Vec<Execution>,
    pub events: Vec<Event>,
}

impl TaskDetail {
    /// The call the task stopped on, while it waits for a person: the
    /// newest execution that ended needing a decision.
    pub fn pending_call(&self) -> Option<&PendingDecision> {
        if self.task.status != TaskStatus::WaitingApproval {
            return None;
        }
        self.executions
            .iter()
            .rev()
            .find(|e| e.outcome == ExecutionOutcome::DecisionRequired)
            .and_then(|e| e.result.pending_decision.as_ref())
    }

    /// The usage of every execution, added up.
    pub fn total_usage(&self) -> Usage {
        self.executions.iter().fold(Usage::default(), |mut sum, e| {
            sum.requests += e.usage.requests;
            sum.prompt_tokens += e.usage.prompt_tokens;
            sum.cached_tokens += e.usage.cached_tokens;
            sum.completion_tokens += e.usage.completion_tokens;
            sum.reasoning_tokens += e.usage.reasoning_tokens;
            sum.tool_calls += e.usage.tool_calls;
            sum.cost_usd += e.usage.cost_usd;
            sum
        })
    }

    /// The newest answer an execution gave.
    pub fn answer(&self) -> Option<&str> {
        self.executions
            .iter()
            .rev()
            .map(|e| e.result.answer.as_str())
            .find(|a| !a.trim().is_empty())
    }

    /// When the task last changed, as the newest of the task and its
    /// events.
    pub fn last_change(&self) -> DateTime<Utc> {
        self.events
            .iter()
            .map(|e| e.created_at)
            .fold(self.task.updated_at, DateTime::max)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::test_support::{fleet, run};
    use crate::application::{ControlPlane, LoadTaskDetail};
    use crate::domain::TaskFilter;

    fn detail_of(status: TaskStatus) -> TaskDetail {
        let (control_plane, _) = fleet();
        let tasks = run(control_plane.list_tasks(TaskFilter::default())).unwrap();
        let id = tasks.into_iter().find(|t| t.status == status).unwrap().id;
        run(LoadTaskDetail::new(control_plane).execute(id)).unwrap()
    }

    #[test]
    fn a_completed_task_has_its_answer_and_no_pending_call() {
        let detail = detail_of(TaskStatus::Completed);
        assert_eq!(detail.answer(), Some("There are 3 files."));
        assert!(detail.pending_call().is_none());
        let usage = detail.total_usage();
        assert_eq!(usage.total_tokens(), 12300);
        assert!((usage.cost_usd - 0.0012).abs() < 1e-9);
    }

    #[test]
    fn a_waiting_task_without_a_stopped_execution_has_no_pending_call() {
        let mut detail = detail_of(TaskStatus::WaitingApproval);
        detail.executions.clear();
        assert!(detail.pending_call().is_none());
        assert_eq!(detail.total_usage(), Usage::default());
        detail.events.clear();
        assert_eq!(detail.last_change(), detail.task.updated_at);
    }
}
