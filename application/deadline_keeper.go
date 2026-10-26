package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// deadlineKeeper fails a delegated task whose deadline came before it
// ended: one still Pending, one waiting for a person (it ends Expired),
// or one whose next execution would start too late. The execution in
// flight, if any, ends with it.
type deadlineKeeper struct {
	deps Dependencies
	rec  recorder
}

func newDeadlineKeeper(deps Dependencies) deadlineKeeper {
	return deadlineKeeper{deps: deps, rec: recorder{deps: deps}}
}

// miss fails the task, and the execution when it is still running.
func (k deadlineKeeper) miss(ctx context.Context, bot domain.Bot, task *domain.Task, execution *domain.Execution) error {
	now := k.deps.Clock.Now()
	lineage, _ := task.Lineage()
	if err := task.MissDeadline(now); err != nil {
		return err
	}
	var executionID domain.ExecutionID
	if execution != nil && execution.Outcome() == domain.ExecutionRunning {
		if err := execution.Fail(task.FailureReason(), domain.Result{}, domain.Usage{}, "", now); err != nil {
			return err
		}
		if err := k.deps.Executions.Save(ctx, *execution); err != nil {
			return err
		}
		executionID = execution.ID()
	}
	if err := k.deps.Tasks.Save(ctx, *task); err != nil {
		return err
	}
	payload := map[string]any{"reason": "deadline", "deadline": lineage.Grant.Deadline, "parentTaskId": lineage.ParentTaskID}
	if executionID != "" {
		payload["executionId"] = executionID.String()
	}
	if err := k.rec.executionEvent(ctx, endEvent(*task), bot, task.ID(), executionID, task.CorrelationID(), payload); err != nil {
		return err
	}
	return k.rec.audit(ctx, "task.deadline", task.ID().String(), domain.AuditOK, map[string]string{"parentTask": lineage.ParentTaskID})
}
