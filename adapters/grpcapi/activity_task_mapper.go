package grpcapi

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

// activityTask maps a task to the wire.
func activityTask(t domain.Task) *operatorv1.Task {
	s := t.Snapshot()
	out := &operatorv1.Task{
		Id: s.ID, BotId: s.BotID, Objective: s.Objective, Status: s.Status, IdempotencyKey: s.IdempotencyKey,
		CorrelationId: s.CorrelationID, Trigger: &operatorv1.TriggerRef{Type: s.Trigger.Type, Name: s.Trigger.Name},
		Input: s.Input, ExecutionIds: s.ExecutionIDs, FailureReason: s.FailureReason, Children: s.Children,
		GrantedModelCalls: int32(s.GrantedModel), GrantedToolCalls: int32(s.GrantedTools), Priority: s.Priority,
		Waiting: s.Waiting, CreatedAt: activityTime(s.CreatedAt), UpdatedAt: activityTime(s.UpdatedAt),
	}
	for _, pin := range s.Skills {
		out.Skills = append(out.Skills, &operatorv1.SkillPin{Name: pin.Name, Version: pin.Version, Catalog: pin.Catalog})
	}
	if a := s.Approval; a != nil {
		out.Approval = &operatorv1.ApprovalWait{
			CallId: a.CallID, Tool: a.Tool, Since: activityTime(a.Since), Deadline: activityTime(a.Deadline),
			OnExpire: a.OnExpire.String(), Outcome: a.Outcome,
		}
		if a.ClosedAt != nil {
			out.Approval.ClosedAt = activityTime(*a.ClosedAt)
		}
	}
	if l := s.Delegation; l != nil {
		out.Delegation = &operatorv1.TaskLineage{
			ParentTaskId: l.ParentTaskID, ParentExecutionId: l.ParentExecutionID, ParentBotId: l.ParentBotID,
			Depth: int32(l.Depth), MaxDepth: int32(l.MaxDepth), Chain: l.Chain, EventId: l.EventID,
			Grant: &operatorv1.DelegationGrant{
				MaxModelCalls: int32(l.Grant.MaxModelCalls), MaxToolCalls: int32(l.Grant.MaxToolCalls),
				Deadline: activityTime(l.Grant.Deadline),
			},
		}
	}
	return out
}
