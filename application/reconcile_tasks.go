package application

import (
	"context"
	"fmt"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ReconcileGrace is what the default grace period adds to a bot's task
// timeout: an execution older than that has outlived any process that
// could still report it.
const ReconcileGrace = time.Minute

// ReconcileTasks ends the tasks a dead process left Running (vision
// §14.6). RunTask persists a task as Running before it calls the runtime,
// so a crash leaves a Running task on disk and nothing else ends it. This
// use case finds every Running task whose execution started more than a
// grace period ago and fails it with a diagnosis: the loss is explicit
// and no task stays Running forever. A younger task may still be running
// in another process and is left alone.
type ReconcileTasks struct {
	deps Dependencies
	rec  recorder
}

// NewReconcileTasks wires the use case.
func NewReconcileTasks(deps Dependencies) ReconcileTasks {
	return ReconcileTasks{deps: deps, rec: recorder{deps: deps}}
}

// Execute fails every Running task older than the grace period and
// reports which tasks it failed and which it skipped. A grace of zero or
// less means each bot's own: its task timeout (DefaultTaskTimeout when it
// sets none) plus ReconcileGrace. A task already failed stays failed if a
// later task's reconciliation fails with an error; running again is safe.
func (uc ReconcileTasks) Execute(ctx context.Context, grace time.Duration) (ReconcileTasksResult, error) {
	tasks, err := uc.deps.Tasks.ListByStatus(ctx, domain.TaskRunning)
	if err != nil {
		return ReconcileTasksResult{}, err
	}
	result := ReconcileTasksResult{Failed: []domain.TaskID{}, Skipped: []domain.TaskID{}}
	for _, task := range tasks {
		failed, err := uc.reconcile(ctx, task, grace)
		if err != nil {
			return ReconcileTasksResult{}, fmt.Errorf("reconcile task %s: %w", task.ID(), err)
		}
		if failed {
			result.Failed = append(result.Failed, task.ID())
		} else {
			result.Skipped = append(result.Skipped, task.ID())
		}
	}
	return result, nil
}

// reconcile fails one Running task when its execution is older than the
// grace period, and reports whether it did. The bot is read for its
// budgets and its tenant, never for its status: a paused bot's lost task
// is still lost.
func (uc ReconcileTasks) reconcile(ctx context.Context, task domain.Task, grace time.Duration) (bool, error) {
	bot, err := uc.deps.Bots.Get(ctx, task.BotID())
	if err != nil {
		return false, err
	}
	execution, err := uc.deps.Executions.Get(ctx, task.CurrentExecutionID())
	if err != nil {
		return false, err
	}
	if grace <= 0 {
		grace = bot.Budgets().TaskTimeoutOr(DefaultTaskTimeout) + ReconcileGrace
	}
	now := uc.deps.Clock.Now()
	age := now.Sub(execution.StartedAt())
	expired := execution.LeaseExpired(now)
	if age <= grace && !expired {
		return false, nil
	}
	reason := diagnosis(execution, age, expired)
	if execution.Outcome() == domain.ExecutionRunning {
		tracePath, _ := uc.deps.Traces.TracePath(execution.ID())
		if err := execution.Fail(reason, domain.Result{}, domain.Usage{}, tracePath, now); err != nil {
			return false, err
		}
		if err := uc.deps.Executions.Save(ctx, execution); err != nil {
			return false, err
		}
	}
	if err := task.Fail(reason, now); err != nil {
		return false, err
	}
	if err := uc.deps.Tasks.Save(ctx, task); err != nil {
		return false, err
	}
	payload := map[string]any{"executionId": execution.ID().String(), "reason": "reconciled"}
	if err := uc.rec.event(ctx, domain.EventTaskFailed, bot, task.ID(), task.CorrelationID(), payload); err != nil {
		return false, err
	}
	details := map[string]string{"execution": execution.ID().String(), "age": age.Round(time.Second).String()}
	if err := uc.rec.audit(ctx, "task.reconcile", task.ID().String(), domain.AuditOK, details); err != nil {
		return false, err
	}
	return true, nil
}

// diagnosis is the failure reason a reconciled task and execution carry.
// An execution that had already ended when its task was still Running
// (the process died between the two saves) is named as such.
func diagnosis(execution domain.Execution, age time.Duration, leaseExpired bool) string {
	found := "the control plane found it Running after " + age.Round(time.Second).String()
	if execution.Outcome() != domain.ExecutionRunning {
		found += " although its execution " + execution.ID().String() + " had already ended " + execution.Outcome().String()
	}
	if leaseExpired {
		return "reconciled: " + found + "; node " + execution.NodeID().String() + " stopped renewing its lease at " + execution.LeaseUntil().UTC().Format(time.RFC3339)
	}
	return "reconciled: " + found + "; the process that ran it is gone"
}
