package grpcapi

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// executionToProto is one attempt at a task.
func executionToProto(s domain.ExecutionSnapshot) *operatorv1.Execution {
	execution := &operatorv1.Execution{
		Id: s.ID, TaskId: s.TaskID, BotId: s.BotID, StartedAt: timestamp(s.StartedAt), EndedAt: timestampOf(s.EndedAt),
		Outcome: s.Outcome, Result: resultToProto(s.Result), Usage: usageToProto(s.Usage), FailureReason: s.FailureReason,
		TracePath: s.TracePath, Kind: s.Kind, Decision: s.Decision, NodeId: s.NodeID, LeaseUntil: timestampOf(s.LeaseUntil), Receipt: s.Receipt,
	}
	if p := s.Policy; p != nil {
		execution.Policy = &operatorv1.PolicyRecord{Level: p.Level, AllowedTools: p.AllowedTools, RuntimeLevel: p.RuntimeLevel, Enforced: p.Enforced, Path: p.Path, Delegation: p.Delegation}
	}
	if c := s.Correlation; c != nil {
		execution.Correlation = &operatorv1.CorrelationRecord{BotId: c.BotID, TaskId: c.TaskID, ExecutionId: c.ExecutionID, TraceId: c.TraceID, Carried: c.Carried, Path: c.Path}
	}
	return execution
}

func executionOptionalToProto(s *domain.ExecutionSnapshot) *operatorv1.Execution {
	if s == nil {
		return nil
	}
	return executionToProto(*s)
}

func executionsToProto(snapshots []domain.ExecutionSnapshot) []*operatorv1.Execution {
	out := make([]*operatorv1.Execution, 0, len(snapshots))
	for _, s := range snapshots {
		out = append(out, executionToProto(s))
	}
	return out
}

func resultToProto(r domain.Result) *operatorv1.Result {
	result := &operatorv1.Result{Answer: r.Answer, SessionId: r.SessionID, Sessions: r.Sessions, NotStarted: r.NotStarted}
	for _, a := range r.Artifacts {
		result.Artifacts = append(result.Artifacts, artifactToProto(a))
	}
	for _, c := range r.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, &operatorv1.ToolCall{Name: c.Name, CallId: c.CallID, Decision: c.Decision, Status: c.Status, Preview: c.Preview})
	}
	if p := r.PendingDecision; p != nil {
		result.PendingDecision = &operatorv1.PendingDecision{Tool: p.Tool, CallId: p.CallID, SessionId: p.SessionID, Arguments: p.Arguments}
	}
	return result
}

func artifactToProto(a domain.Artifact) *operatorv1.Artifact {
	return &operatorv1.Artifact{
		Name: a.Name, MediaType: a.MediaType, Reference: a.Reference, Size: a.Size, Sha256: a.SHA256, Description: a.Description,
		Source: a.Source, SessionId: a.SessionID, CallId: a.CallID, Stored: a.Stored,
	}
}

func usageToProto(u domain.Usage) *operatorv1.Usage {
	return &operatorv1.Usage{
		Requests: int32(u.Requests), PromptTokens: int32(u.PromptTokens), CachedTokens: int32(u.CachedTokens),
		CompletionTokens: int32(u.CompletionTokens), ReasoningTokens: int32(u.ReasoningTokens), ToolCalls: int32(u.ToolCalls),
		CostUsd: u.CostUSD, CostUnknown: u.CostUnknown,
	}
}

func transcriptRecordToProto(r application.TranscriptRecord) *operatorv1.TranscriptRecord {
	return &operatorv1.TranscriptRecord{
		Seq: r.Seq, ExecutionId: string(r.Execution), TaskId: string(r.Task), BotId: string(r.Bot), Session: r.Session,
		Size: r.Size, Sha256: r.SHA256, Filtered: r.Filtered, SentSha256: r.SentSHA256, ReceivedAt: timestamp(r.ReceivedAt),
	}
}
