use chrono::{DateTime, Duration, Utc};
use serde::{Deserialize, Serialize};

use super::{
    BotId, Correlation, ExecutionId, ExecutionKind, ExecutionOutcome, ExecutionResult, NodeId, PolicyRecord, RunStep,
    TaskId, Usage,
};

/// One run of a task, or of a decision on it, on a node.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Execution {
    pub id: ExecutionId,
    pub task_id: TaskId,
    pub bot_id: BotId,
    pub started_at: DateTime<Utc>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub ended_at: Option<DateTime<Utc>>,
    pub outcome: ExecutionOutcome,
    #[serde(default)]
    pub result: ExecutionResult,
    #[serde(default)]
    pub usage: Usage,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub failure_reason: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub trace_path: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub policy: Option<PolicyRecord>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub correlation: Option<Correlation>,
    #[serde(default)]
    pub kind: ExecutionKind,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub decision: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub node_id: Option<NodeId>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub lease_until: Option<DateTime<Utc>>,
    /// The runtime's receipt of the run, as it wrote it.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub receipt: Option<serde_json::Value>,
}

impl Execution {
    /// How long the execution ran, or has run until `now`.
    pub fn duration(&self, now: DateTime<Utc>) -> Duration {
        (self.ended_at.unwrap_or(now) - self.started_at).max(Duration::zero())
    }

    /// Its tool calls in order: from the receipt, with their timing, when
    /// it has one; else from the result.
    pub fn steps(&self) -> Vec<RunStep> {
        let from_receipt = self.receipt.as_ref().map(RunStep::from_receipt).unwrap_or_default();
        if !from_receipt.is_empty() {
            return from_receipt;
        }
        self.result.tool_calls.iter().map(RunStep::from_call).collect()
    }

    /// Where it ran: the node, or the control plane itself.
    pub fn place(&self) -> &str {
        self.node_id.as_ref().map(NodeId::as_str).unwrap_or("control plane")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads_an_execution_with_a_pending_call() {
        let execution: Execution = serde_json::from_str(
            r#"{"id":"exec-1","taskId":"task-1","botId":"bot-a","startedAt":"2026-10-24T16:04:00Z",
            "endedAt":"2026-10-24T16:04:09Z","outcome":"DecisionRequired",
            "result":{"answer":"","toolCalls":[{"name":"local_write","callId":"c1","decision":"ask","status":"pending"}],
            "pendingDecision":{"tool":"local_write","callId":"c1","sessionId":"s1","arguments":{"path":"a.txt"}}},
            "usage":{"requests":2,"costUsd":0.001},"kind":"run","nodeId":"gx10-ff95",
            "correlation":{"botId":"bot-a","taskId":"task-1","executionId":"exec-1","traceId":"trace-1","carried":true},
            "policy":{"level":"act-with-approval","enforced":true}}"#,
        )
        .unwrap();
        assert_eq!(execution.outcome, ExecutionOutcome::DecisionRequired);
        assert_eq!(execution.result.pending_decision.as_ref().unwrap().tool, "local_write");
        assert_eq!(execution.place(), "gx10-ff95");
        let now: DateTime<Utc> = "2026-10-24T17:00:00Z".parse().unwrap();
        assert_eq!(execution.duration(now).num_seconds(), 9);
        assert!(execution.correlation.as_ref().unwrap().carried);
        assert!(execution.policy.as_ref().unwrap().enforced);
        let steps = execution.steps();
        assert_eq!(
            (steps.len(), steps[0].why()),
            (1, "It asked you"),
            "no receipt: the result's calls"
        );
    }

    #[test]
    fn a_running_execution_runs_until_now() {
        let execution: Execution = serde_json::from_str(
            r#"{"id":"exec-2","taskId":"task-1","botId":"bot-a","startedAt":"2026-10-24T16:00:00Z","outcome":"Running","kind":""}"#,
        )
        .unwrap();
        let now: DateTime<Utc> = "2026-10-24T16:01:30Z".parse().unwrap();
        assert_eq!(execution.duration(now).num_seconds(), 90);
        assert_eq!(execution.place(), "control plane");
        assert_eq!(execution.kind, ExecutionKind::Run);
    }
}
