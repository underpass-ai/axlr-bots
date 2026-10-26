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
	if task.Status().IsTerminal() {
		refused := domain.StateError{Subject: "task " + id.String(), Current: task.Status().String(), Attempted: "cancel"}
		_ = uc.rec.audit(ctx, "task.cancel", id.String(), domain.AuditDenied, map[string]string{"reason": refused.Error()})
		return CancelTaskResult{}, refused
	}
	bot, err := uc.deps.Bots.Get(ctx, task.BotID())
	if err != nil {
		return CancelTaskResult{}, err
	}
	return stopTask(ctx, uc.deps, bot, task, "cancelled by "+uc.deps.Actor, domain.EventTaskCancelled, "task.cancel", nil)
}
