package grpcapi

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

// taskToProto is a task as an operator sees it.
func taskToProto(s domain.TaskSnapshot) *operatorv1.Task {
	task := &operatorv1.Task{
		Id: s.ID, BotId: s.BotID, Objective: s.Objective, Status: s.Status, IdempotencyKey: s.IdempotencyKey,
		CorrelationId: s.CorrelationID, Trigger: &operatorv1.TriggerRef{Type: s.Trigger.Type, Name: s.Trigger.Name},
		Input: s.Input, ExecutionIds: s.ExecutionIDs, FailureReason: s.FailureReason, Children: s.Children,
		GrantedModelCalls: int32(s.GrantedModel), GrantedToolCalls: int32(s.GrantedTools), Priority: s.Priority,
		Skills: skillsToProto(s.Skills), Waiting: s.Waiting, CreatedAt: timestamp(s.CreatedAt), UpdatedAt: timestamp(s.UpdatedAt),
	}
	if a := s.Approval; a != nil {
		task.Approval = &operatorv1.ApprovalWait{
			CallId: a.CallID, Tool: a.Tool, Since: timestamp(a.Since), Deadline: timestamp(a.Deadline),
			OnExpire: string(a.OnExpire), Outcome: a.Outcome, ClosedAt: timestampOf(a.ClosedAt),
		}
	}
	if d := s.Delegation; d != nil {
		task.Delegation = &operatorv1.TaskLineage{
			ParentTaskId: d.ParentTaskID, ParentExecutionId: d.ParentExecutionID, ParentBotId: d.ParentBotID,
			Depth: int32(d.Depth), MaxDepth: int32(d.MaxDepth), Chain: d.Chain, EventId: d.EventID,
			Grant: &operatorv1.DelegationGrant{MaxModelCalls: int32(d.Grant.MaxModelCalls), MaxToolCalls: int32(d.Grant.MaxToolCalls), Deadline: timestamp(d.Grant.Deadline)},
		}
	}
	return task
}

func tasksToProto(snapshots []domain.TaskSnapshot) []*operatorv1.Task {
	out := make([]*operatorv1.Task, 0, len(snapshots))
	for _, s := range snapshots {
		out = append(out, taskToProto(s))
	}
	return out
}
