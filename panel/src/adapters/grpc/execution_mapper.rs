use serde_json::{Value, json};

use super::proto;
use super::wire::Wire;
use crate::application::PanelError;
use crate::domain::{Execution, Usage};

/// An execution of the control plane, with what it used, into the panel's.
pub(super) struct ExecutionMapper;

impl ExecutionMapper {
    pub fn execution(message: &proto::Execution) -> Result<Execution, PanelError> {
        Wire::read(Self::shape(message)?, "execution")
    }

    pub fn executions(messages: &[proto::Execution]) -> Result<Vec<Execution>, PanelError> {
        messages.iter().map(Self::execution).collect()
    }

    pub fn usage(usage: Option<&proto::Usage>) -> Result<Usage, PanelError> {
        Wire::read(Self::usage_shape(usage), "usage")
    }

    fn usage_shape(usage: Option<&proto::Usage>) -> Value {
        let u = usage.cloned().unwrap_or_default();
        json!({
            "requests": u.requests.max(0),
            "promptTokens": u.prompt_tokens.max(0),
            "cachedTokens": u.cached_tokens.max(0),
            "completionTokens": u.completion_tokens.max(0),
            "reasoningTokens": u.reasoning_tokens.max(0),
            "toolCalls": u.tool_calls.max(0),
            "costUsd": u.cost_usd,
            "costUnknown": u.cost_unknown,
        })
    }

    fn shape(execution: &proto::Execution) -> Result<Value, PanelError> {
        let result = execution.result.clone().unwrap_or_default();
        Ok(json!({
            "id": execution.id,
            "taskId": execution.task_id,
            "botId": execution.bot_id,
            "startedAt": Wire::time(execution.started_at.as_ref()),
            "endedAt": Wire::time(execution.ended_at.as_ref()),
            "outcome": execution.outcome,
            "result": {
                "answer": result.answer,
                "sessionId": result.session_id,
                "sessions": result.sessions,
                "artifacts": result.artifacts.iter().map(Self::artifact).collect::<Vec<_>>(),
                "toolCalls": result.tool_calls.iter().map(|c| json!({
                    "name": c.name, "callId": c.call_id, "decision": c.decision, "status": c.status,
                })).collect::<Vec<_>>(),
                "pendingDecision": match &result.pending_decision {
                    None => Value::Null,
                    Some(p) => json!({
                        "tool": p.tool,
                        "callId": p.call_id,
                        "sessionId": p.session_id,
                        "arguments": Wire::document(&p.arguments)?,
                    }),
                },
            },
            "usage": Self::usage_shape(execution.usage.as_ref()),
            "failureReason": execution.failure_reason,
            "tracePath": execution.trace_path,
            "policy": execution.policy.as_ref().map(|p| json!({
                "level": p.level,
                "allowedTools": p.allowed_tools,
                "runtimeLevel": p.runtime_level,
                "enforced": p.enforced,
                "path": p.path,
            })),
            "correlation": execution.correlation.as_ref().map(|c| json!({
                "botId": c.bot_id,
                "taskId": c.task_id,
                "executionId": c.execution_id,
                "traceId": c.trace_id,
                "carried": c.carried,
                "path": c.path,
            })),
            "kind": execution.kind,
            "decision": execution.decision,
            "nodeId": Wire::text(&execution.node_id),
            "leaseUntil": Wire::time(execution.lease_until.as_ref()),
            "receipt": Wire::document(&execution.receipt)?,
        }))
    }

    fn artifact(artifact: &proto::Artifact) -> Value {
        json!({
            "name": artifact.name,
            "mediaType": artifact.media_type,
            "reference": artifact.reference,
            "size": artifact.size.max(0),
            "sha256": artifact.sha256,
            "description": artifact.description,
            "source": artifact.source,
            "stored": artifact.stored,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::{ExecutionKind, ExecutionOutcome};

    fn message() -> proto::Execution {
        proto::Execution {
            id: "exec-1".into(),
            task_id: "task-1".into(),
            bot_id: "bot-a".into(),
            started_at: Some(Wire::timestamp("2026-10-25T10:00:00Z".parse().unwrap())),
            outcome: "DecisionRequired".into(),
            result: Some(proto::Result {
                answer: "Two files.".into(),
                artifacts: vec![proto::Artifact {
                    name: "report.md".into(),
                    size: 12,
                    stored: true,
                    ..Default::default()
                }],
                tool_calls: vec![proto::ToolCall {
                    name: "local_write".into(),
                    call_id: "c1".into(),
                    status: "pending".into(),
                    ..Default::default()
                }],
                pending_decision: Some(proto::PendingDecision {
                    tool: "local_write".into(),
                    call_id: "c1".into(),
                    arguments: br#"{"path":"a.txt"}"#.to_vec(),
                    ..Default::default()
                }),
                ..Default::default()
            }),
            usage: Some(proto::Usage {
                requests: 3,
                prompt_tokens: 12000,
                cost_usd: 0.0012,
                ..Default::default()
            }),
            node_id: "wrx80".into(),
            ..Default::default()
        }
    }

    #[test]
    fn reads_an_execution_whole() {
        let execution = ExecutionMapper::execution(&message()).unwrap();
        assert_eq!(execution.outcome, ExecutionOutcome::DecisionRequired);
        assert_eq!(execution.kind, ExecutionKind::Run);
        assert_eq!(execution.node_id.unwrap().as_str(), "wrx80");
        assert_eq!(execution.result.artifacts[0].size, 12);
        let pending = execution.result.pending_decision.unwrap();
        assert_eq!(
            pending.argument_lines(),
            vec![("path".to_string(), "a.txt".to_string())]
        );
        assert_eq!(execution.usage.requests, 3);
        assert!(execution.ended_at.is_none() && execution.receipt.is_none() && execution.policy.is_none());
    }

    #[test]
    fn a_receipt_that_is_not_json_is_unreadable() {
        let mut broken = message();
        broken.receipt = b"{not json".to_vec();
        assert!(matches!(
            ExecutionMapper::execution(&broken),
            Err(PanelError::Unreadable(_))
        ));
    }

    #[test]
    fn an_unknown_cost_stays_unknown() {
        let usage = ExecutionMapper::usage(Some(&proto::Usage {
            cost_unknown: true,
            ..Default::default()
        }))
        .unwrap();
        assert!(usage.cost_unknown);
        assert_eq!(ExecutionMapper::usage(None).unwrap(), Usage::default());
    }
}
