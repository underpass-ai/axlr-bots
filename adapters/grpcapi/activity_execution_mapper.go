package grpcapi

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

// activityExecution maps an execution to the wire.
func activityExecution(e domain.Execution) *operatorv1.Execution {
	s := e.Snapshot()
	out := &operatorv1.Execution{
		Id: s.ID, TaskId: s.TaskID, BotId: s.BotID, StartedAt: activityTime(s.StartedAt), Outcome: s.Outcome,
		Result: activityResult(s.Result), Usage: activityUsage(s.Usage), FailureReason: s.FailureReason,
		TracePath: s.TracePath, Kind: s.Kind, Decision: s.Decision, NodeId: s.NodeID, Receipt: s.Receipt,
	}
	if s.EndedAt != nil {
		out.EndedAt = activityTime(*s.EndedAt)
	}
	if s.LeaseUntil != nil {
		out.LeaseUntil = activityTime(*s.LeaseUntil)
	}
	if p := s.Policy; p != nil {
		out.Policy = &operatorv1.PolicyRecord{
			Level: p.Level, AllowedTools: p.AllowedTools, RuntimeLevel: p.RuntimeLevel,
			Enforced: p.Enforced, Path: p.Path, Delegation: p.Delegation,
		}
	}
	if c := s.Correlation; c != nil {
		out.Correlation = &operatorv1.CorrelationRecord{
			BotId: c.BotID, TaskId: c.TaskID, ExecutionId: c.ExecutionID, TraceId: c.TraceID, Carried: c.Carried, Path: c.Path,
		}
	}
	return out
}

// activityResult maps an execution's result to the wire.
func activityResult(r domain.Result) *operatorv1.Result {
	out := &operatorv1.Result{Answer: r.Answer, SessionId: r.SessionID, Sessions: r.Sessions, NotStarted: r.NotStarted}
	for _, a := range r.Artifacts {
		out.Artifacts = append(out.Artifacts, &operatorv1.Artifact{
			Name: a.Name, MediaType: a.MediaType, Reference: a.Reference, Size: a.Size, Sha256: a.SHA256,
			Description: a.Description, Source: a.Source, SessionId: a.SessionID, CallId: a.CallID, Stored: a.Stored,
		})
	}
	for _, c := range r.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, &operatorv1.ToolCall{Name: c.Name, CallId: c.CallID, Decision: c.Decision, Status: c.Status, Preview: c.Preview})
	}
	if p := r.PendingDecision; p != nil {
		out.PendingDecision = &operatorv1.PendingDecision{Tool: p.Tool, CallId: p.CallID, SessionId: p.SessionID, Arguments: p.Arguments}
	}
	return out
}
