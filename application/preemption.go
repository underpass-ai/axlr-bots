package application

import (
	"context"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// preemptFor cancels, for an urgent task, the bot's tasks in progress
// that are not urgent: a Running task's execution ends Cancelled (a node
// stops it at its next lease renewal), a WaitingApproval task's wait is
// closed so a late decision authorises nothing. Pending tasks wait
// behind the urgent one; another urgent task is never cancelled.
func preemptFor(ctx context.Context, deps Dependencies, bot domain.Bot, urgent domain.Task, why string) error {
	tasks, err := deps.Tasks.List(ctx)
	if err != nil {
		return err
	}
	reason := "preempted by the urgent task " + urgent.ID().String()
	if why != "" {
		reason += " (" + why + ")"
	}
	for _, task := range tasks {
		if task.BotID() != bot.ID() || task.ID() == urgent.ID() || task.IsUrgent() {
			continue
		}
		if task.Status() != domain.TaskRunning && task.Status() != domain.TaskWaitingApproval {
			continue
		}
		extra := map[string]any{"preemptedBy": urgent.ID().String(), "why": why}
		if _, err := stopTask(ctx, deps, bot, task, reason, domain.EventTaskPreempted, "task.preempt", extra); err != nil {
			return err
		}
	}
	return nil
}

// stopTask cancels one task for a reason: its running execution ends
// Cancelled, the task keeps the reason, and the event and the audit
// entry name it. CancelTask, Stop and preemption end tasks through it.
func stopTask(ctx context.Context, deps Dependencies, bot domain.Bot, task domain.Task, reason string, event domain.EventType, action string, extra map[string]any) (CancelTaskResult, error) {
	rec := recorder{deps: deps}
	now := deps.Clock.Now()
	if err := task.CancelBecause(reason, now); err != nil {
		_ = rec.audit(ctx, action, task.ID().String(), domain.AuditDenied, map[string]string{"reason": err.Error()})
		return CancelTaskResult{}, err
	}
	payload := map[string]any{"reason": reason}
	for key, value := range extra {
		payload[key] = value
	}
	details := map[string]string{"reason": reason}
	cancelled, err := cancelExecution(ctx, deps, task, reason, now)
	if err != nil {
		return CancelTaskResult{}, err
	}
	var executionID domain.ExecutionID
	if cancelled != nil {
		executionID = cancelled.ID()
		payload["executionId"], details["execution"] = executionID.String(), executionID.String()
		if cancelled.IsAssigned() {
			payload["nodeId"] = cancelled.NodeID().String()
		}
	}
	if err := deps.Tasks.Save(ctx, task); err != nil {
		return CancelTaskResult{}, err
	}
	if err := rec.executionEvent(ctx, event, bot, task.ID(), executionID, task.CorrelationID(), payload); err != nil {
		return CancelTaskResult{}, err
	}
	if err := rec.audit(ctx, action, task.ID().String(), domain.AuditOK, details); err != nil {
		return CancelTaskResult{}, err
	}
	return CancelTaskResult{Task: task, Execution: cancelled, Changed: true}, nil
}

// cancelExecution ends the task's current execution Cancelled when it
// still runs; nil when there was none.
func cancelExecution(ctx context.Context, deps Dependencies, task domain.Task, reason string, now time.Time) (*domain.Execution, error) {
	current := task.CurrentExecutionID()
	if current == "" {
		return nil, nil
	}
	execution, err := deps.Executions.Get(ctx, current)
	if err != nil {
		return nil, err
	}
	if execution.Outcome() != domain.ExecutionRunning {
		return nil, nil
	}
	if err := execution.Cancel(reason, now); err != nil {
		return nil, err
	}
	if err := deps.Executions.Save(ctx, execution); err != nil {
		return nil, err
	}
	return &execution, nil
}
