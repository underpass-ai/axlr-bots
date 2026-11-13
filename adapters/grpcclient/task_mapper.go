package grpcclient

import (
	"encoding/json"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

// taskSnapshot is a task as the API answers it.
func taskSnapshot(task *operatorv1.Task) domain.TaskSnapshot {
	snapshot := domain.TaskSnapshot{
		ID: task.GetId(), BotID: task.GetBotId(), Objective: task.GetObjective(), Status: task.GetStatus(),
		IdempotencyKey: task.GetIdempotencyKey(), CorrelationID: task.GetCorrelationId(),
		Trigger:      domain.TriggerRefSnapshot{Type: task.GetTrigger().GetType(), Name: task.GetTrigger().GetName()},
		ExecutionIDs: task.GetExecutionIds(), FailureReason: task.GetFailureReason(), Children: task.GetChildren(),
		GrantedModel: int(task.GetGrantedModelCalls()), GrantedTools: int(task.GetGrantedToolCalls()), Priority: task.GetPriority(),
		Skills: skillPinSnapshots(task.GetSkills()), Waiting: task.GetWaiting(),
		CreatedAt: timeOf(task.GetCreatedAt()), UpdatedAt: timeOf(task.GetUpdatedAt()),
	}
	if snapshot.ExecutionIDs == nil {
		snapshot.ExecutionIDs = []string{}
	}
	if input := task.GetInput(); len(input) > 0 {
		snapshot.Input = json.RawMessage(input)
	}
	if wait := task.GetApproval(); wait != nil {
		snapshot.Approval = &domain.ApprovalWait{
			CallID: wait.GetCallId(), Tool: wait.GetTool(), Since: timeOf(wait.GetSince()), Deadline: timeOf(wait.GetDeadline()),
			OnExpire: domain.ApprovalExpiry(wait.GetOnExpire()), Outcome: wait.GetOutcome(), ClosedAt: timePtr(wait.GetClosedAt()),
		}
	}
	if lineage := task.GetDelegation(); lineage != nil {
		snapshot.Delegation = &domain.TaskLineage{
			ParentTaskID: lineage.GetParentTaskId(), ParentExecutionID: lineage.GetParentExecutionId(), ParentBotID: lineage.GetParentBotId(),
			Depth: int(lineage.GetDepth()), MaxDepth: int(lineage.GetMaxDepth()), Chain: lineage.GetChain(), EventID: lineage.GetEventId(),
			Grant: domain.DelegationGrant{MaxModelCalls: int(lineage.GetGrant().GetMaxModelCalls()), MaxToolCalls: int(lineage.GetGrant().GetMaxToolCalls()), Deadline: timeOf(lineage.GetGrant().GetDeadline())},
		}
	}
	return snapshot
}

func taskSnapshots(tasks []*operatorv1.Task) []domain.TaskSnapshot {
	out := make([]domain.TaskSnapshot, len(tasks))
	for i, task := range tasks {
		out[i] = taskSnapshot(task)
	}
	return out
}
