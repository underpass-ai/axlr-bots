package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// CancelTask stops a task that has not finished: a Pending task never
// runs, a Running task's execution is ended Cancelled (the process that
// runs it is then interrupted by the runner, or stops on the node when
// its lease is refused, and its report is dropped), a WaitingApproval
// task never gets its decision. Cancelling a Cancelled task changes
// nothing; a Completed or Failed task is refused.
type CancelTask struct {
	deps Dependencies
	rec  recorder
}

// NewCancelTask wires the use case.
func NewCancelTask(deps Dependencies) CancelTask {
	return CancelTask{deps: deps, rec: recorder{deps: deps}}
}

// Execute cancels the task.
func (uc CancelTask) Execute(ctx context.Context, id domain.TaskID) (CancelTaskResult, error) {
	task, err := uc.deps.Tasks.Get(ctx, id)
	if err != nil {
		return CancelTaskResult{}, err
	}
	if task.Status() == domain.TaskCancelled {
		return CancelTaskResult{Task: task}, nil
	}
	now := uc.deps.Clock.Now()
	if err := task.Cancel(now); err != nil {
		_ = uc.rec.audit(ctx, "task.cancel", id.String(), domain.AuditDenied, map[string]string{"reason": err.Error()})
		return CancelTaskResult{}, err
	}
	bot, err := uc.deps.Bots.Get(ctx, task.BotID())
	if err != nil {
		return CancelTaskResult{}, err
	}
	reason := "cancelled by " + uc.deps.Actor
	payload := map[string]any{"reason": reason}
	details := map[string]string{}
	var cancelled *domain.Execution
	if current := task.CurrentExecutionID(); current != "" {
		execution, err := uc.deps.Executions.Get(ctx, current)
		if err != nil {
			return CancelTaskResult{}, err
		}
		if execution.Outcome() == domain.ExecutionRunning {
			if err := execution.Cancel(reason, now); err != nil {
				return CancelTaskResult{}, err
			}
			if err := uc.deps.Executions.Save(ctx, execution); err != nil {
				return CancelTaskResult{}, err
			}
			cancelled = &execution
			payload["executionId"] = execution.ID().String()
			details["execution"] = execution.ID().String()
			if execution.IsAssigned() {
				payload["nodeId"] = execution.NodeID().String()
			}
		}
	}
	if err := uc.deps.Tasks.Save(ctx, task); err != nil {
		return CancelTaskResult{}, err
	}
	var executionID domain.ExecutionID
	if cancelled != nil {
		executionID = cancelled.ID()
	}
	if err := uc.rec.executionEvent(ctx, domain.EventTaskCancelled, bot, task.ID(), executionID, task.CorrelationID(), payload); err != nil {
		return CancelTaskResult{}, err
	}
	if err := uc.rec.audit(ctx, "task.cancel", id.String(), domain.AuditOK, details); err != nil {
		return CancelTaskResult{}, err
	}
	return CancelTaskResult{Task: task, Execution: cancelled, Changed: true}, nil
}
