package grpcclient

import (
	"encoding/json"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

// executionSnapshot is an execution as the API answers it.
func executionSnapshot(execution *operatorv1.Execution) domain.ExecutionSnapshot {
	snapshot := domain.ExecutionSnapshot{
		ID: execution.GetId(), TaskID: execution.GetTaskId(), BotID: execution.GetBotId(), StartedAt: timeOf(execution.GetStartedAt()),
		EndedAt: timePtr(execution.GetEndedAt()), Outcome: execution.GetOutcome(), Result: resultOf(execution.GetResult()),
		Usage: usageOf(execution.GetUsage()), FailureReason: execution.GetFailureReason(), TracePath: execution.GetTracePath(),
		Kind: execution.GetKind(), Decision: execution.GetDecision(), NodeID: execution.GetNodeId(), LeaseUntil: timePtr(execution.GetLeaseUntil()),
	}
	if policy := execution.GetPolicy(); policy != nil {
		snapshot.Policy = &domain.PolicyRecord{
			Level: policy.GetLevel(), AllowedTools: policy.GetAllowedTools(), RuntimeLevel: policy.GetRuntimeLevel(),
			Enforced: policy.GetEnforced(), Path: policy.GetPath(), Delegation: policy.GetDelegation(),
		}
	}
	if c := execution.GetCorrelation(); c != nil {
		snapshot.Correlation = &domain.CorrelationRecord{BotID: c.GetBotId(), TaskID: c.GetTaskId(), ExecutionID: c.GetExecutionId(), TraceID: c.GetTraceId(), Carried: c.GetCarried(), Path: c.GetPath()}
	}
	if receipt := execution.GetReceipt(); len(receipt) > 0 {
		snapshot.Receipt = json.RawMessage(receipt)
	}
	return snapshot
}

func executionSnapshots(executions []*operatorv1.Execution) []domain.ExecutionSnapshot {
	out := make([]domain.ExecutionSnapshot, len(executions))
	for i, execution := range executions {
		out[i] = executionSnapshot(execution)
	}
	return out
}

// executionPtr is an execution that may be absent, as the cancel answer
// has it.
func executionPtr(execution *operatorv1.Execution) *domain.ExecutionSnapshot {
	if execution == nil {
		return nil
	}
	snapshot := executionSnapshot(execution)
	return &snapshot
}

func resultOf(result *operatorv1.Result) domain.Result {
	out := domain.Result{Answer: result.GetAnswer(), SessionID: result.GetSessionId(), Sessions: result.GetSessions(), NotStarted: result.GetNotStarted()}
	for _, artifact := range result.GetArtifacts() {
		out.Artifacts = append(out.Artifacts, artifactOf(artifact))
	}
	for _, call := range result.GetToolCalls() {
		out.ToolCalls = append(out.ToolCalls, domain.ToolCall{Name: call.GetName(), CallID: call.GetCallId(), Decision: call.GetDecision(), Status: call.GetStatus(), Preview: call.GetPreview()})
	}
	if pending := result.GetPendingDecision(); pending != nil {
		out.PendingDecision = &domain.PendingDecision{Tool: pending.GetTool(), CallID: pending.GetCallId(), SessionID: pending.GetSessionId()}
		if arguments := pending.GetArguments(); len(arguments) > 0 {
			out.PendingDecision.Arguments = json.RawMessage(arguments)
		}
	}
	return out
}

func artifactOf(a *operatorv1.Artifact) domain.Artifact {
	return domain.Artifact{
		Name: a.GetName(), MediaType: a.GetMediaType(), Reference: a.GetReference(), Size: a.GetSize(), SHA256: a.GetSha256(),
		Description: a.GetDescription(), Source: a.GetSource(), SessionID: a.GetSessionId(), CallID: a.GetCallId(), Stored: a.GetStored(),
	}
}

func usageOf(u *operatorv1.Usage) domain.Usage {
	return domain.Usage{
		Requests: int(u.GetRequests()), PromptTokens: int(u.GetPromptTokens()), CachedTokens: int(u.GetCachedTokens()),
		CompletionTokens: int(u.GetCompletionTokens()), ReasoningTokens: int(u.GetReasoningTokens()), ToolCalls: int(u.GetToolCalls()),
		CostUSD: u.GetCostUsd(), CostUnknown: u.GetCostUnknown(),
	}
}
