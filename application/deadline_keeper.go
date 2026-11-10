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

// miss fails the task, and the execution when it is still running. A
// Pending task of a placed bot that no node took says what it waited
// for: "...; it was waiting for node web-1, which is Offline".
func (k deadlineKeeper) miss(ctx context.Context, bot domain.Bot, task *domain.Task, execution *domain.Execution) error {
	now := k.deps.Clock.Now()
	lineage, _ := task.Lineage()
	waiting, err := k.waiting(ctx, bot, *task)
	if err != nil {
		return err
	}
	if err := task.MissDeadline(now, waiting); err != nil {
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
	if waiting != "" {
		payload["waiting"] = waiting
	}
	if executionID != "" {
		payload["executionId"] = executionID.String()
	}
	if err := k.rec.executionEvent(ctx, endEvent(*task), bot, task.ID(), executionID, task.CorrelationID(), payload); err != nil {
		return err
	}
	if err := newIncidentAdvancer(k.deps).taskEnded(ctx, bot, *task, domain.Execution{}); err != nil {
		return err
	}
	if err := newAnswerDeliverer(k.deps).taskEnded(ctx, bot, *task, domain.Execution{}); err != nil {
		return err
	}
	if err := newPlatformPublisher(k.deps).taskEnded(ctx, bot, *task, domain.Execution{}); err != nil {
		return err
	}
	return k.rec.audit(ctx, "task.deadline", task.ID().String(), domain.AuditOK, map[string]string{"parentTask": lineage.ParentTaskID})
}

// waiting is what a Pending task waited for when its deadline came: a
// node its bot's placement admits; empty for any other task.
func (k deadlineKeeper) waiting(ctx context.Context, bot domain.Bot, task domain.Task) (string, error) {
	if task.Status() != domain.TaskPending || bot.Placement().IsEmpty() {
		return "", nil
	}
	nodes, err := k.deps.Nodes.List(ctx)
	if err != nil {
		return "", err
	}
	waiting, _ := placementWait(bot, task.Skills(), nodes)
	return waiting, nil
}
