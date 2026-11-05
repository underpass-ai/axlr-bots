use chrono::{DateTime, Utc};

use super::WaitingCall;
use crate::domain::{Cost, Event, Execution, PendingDecision, Task, TaskStatus, Usage};

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
        WaitingCall::newest(&self.executions)
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
            sum.cost_unknown |= e.usage.cost_unknown;
            sum
        })
    }

    /// What every execution cost, with how many did not tell it.
    pub fn cost(&self) -> Cost {
        self.executions
            .iter()
            .fold(Cost::default(), |sum, e| sum.plus(e.usage.cost()))
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
    use crate::domain::{Decision, TaskFilter};

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
    fn a_run_stopped_before_it_told_its_cost_makes_the_total_a_floor() {
        let (control_plane, _) = fleet();
        assert!(!detail_of(TaskStatus::Completed).cost().is_floor());
        let tasks = run(control_plane.list_tasks(TaskFilter::default())).unwrap();
        let id = tasks
            .into_iter()
            .find(|t| t.status == TaskStatus::WaitingApproval)
            .unwrap()
            .id;
        let since = "2000-01-01T00:00:00Z".parse().unwrap();
        let before = run(control_plane.overview(since)).unwrap().cost_since();
        // Approved, its decision runs; stopped then, it never says what it spent.
        run(control_plane.decide_task(id.clone(), Decision::Approve, None)).unwrap();
        run(control_plane.cancel_task(id.clone())).unwrap();
        let detail = run(LoadTaskDetail::new(control_plane.clone()).execute(id)).unwrap();
        assert_eq!(detail.cost().unknown_runs, 1);
        assert!(detail.total_usage().cost_unknown);
        let after = run(control_plane.overview(since)).unwrap().cost_since();
        assert_eq!(after.unknown_runs, before.unknown_runs + 1);
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
