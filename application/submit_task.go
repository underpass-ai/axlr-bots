package application

import (
	"context"

	"github.com/underpass-ai/underpass-bots/domain"
)

// SubmitTask accepts a task for an enabled bot. It is idempotent on the
// key: resubmitting the same key returns the same task and starts nothing.
type SubmitTask struct {
	deps Dependencies
	rec  recorder
}

// NewSubmitTask wires the use case.
func NewSubmitTask(deps Dependencies) SubmitTask {
	return SubmitTask{deps: deps, rec: recorder{deps: deps}}
}

// Execute creates the task, or returns the one the key already names.
func (uc SubmitTask) Execute(ctx context.Context, command SubmitTaskCommand) (SubmitTaskResult, error) {
	botID, err := domain.NewBotID(command.BotID)
	if err != nil {
		return SubmitTaskResult{}, err
	}
	objective, err := domain.NewObjective(command.Objective)
	if err != nil {
		return SubmitTaskResult{}, err
	}
	key, err := domain.NewIdempotencyKey(command.IdempotencyKey)
	if err != nil {
		return SubmitTaskResult{}, err
	}
	correlation, err := domain.NewCorrelationID(command.CorrelationID)
	if err != nil {
		return SubmitTaskResult{}, err
	}
	bot, err := uc.deps.Bots.Get(ctx, botID)
	if err != nil {
		return SubmitTaskResult{}, err
	}
	if existing, found, err := uc.deps.Tasks.FindByIdempotencyKey(ctx, botID, key); err != nil {
		return SubmitTaskResult{}, err
	} else if found {
		if err := uc.rec.audit(ctx, "task.submit", existing.ID().String(), domain.AuditOK, map[string]string{"idempotencyKey": key.String(), "deduplicated": "true"}); err != nil {
			return SubmitTaskResult{}, err
		}
		return SubmitTaskResult{Task: existing, Created: false}, nil
	}
	if !bot.AcceptsTasks() {
		_ = uc.rec.audit(ctx, "task.submit", botID.String(), domain.AuditDenied, map[string]string{"reason": "bot is " + bot.Status().String()})
		return SubmitTaskResult{}, domain.StateError{Subject: "bot " + botID.String(), Current: bot.Status().String(), Attempted: "accept a task"}
	}
	if !bot.AcceptsTrigger(domain.TriggerManual) {
		_ = uc.rec.audit(ctx, "task.submit", botID.String(), domain.AuditDenied, map[string]string{"reason": "bot takes no manual tasks"})
		return SubmitTaskResult{}, domain.StateError{Subject: "bot " + botID.String(), Current: "without a manual trigger", Attempted: "accept a manual task"}
	}
	taskID, err := domain.NewTaskID(uc.deps.IDs.NewID("task-"))
	if err != nil {
		return SubmitTaskResult{}, err
	}
	task, err := domain.NewTask(taskID, botID, objective, key, correlation, uc.deps.Clock.Now())
	if err != nil {
		return SubmitTaskResult{}, err
	}
	if err := uc.deps.Tasks.Save(ctx, task); err != nil {
		return SubmitTaskResult{}, err
	}
	if err := uc.rec.event(ctx, domain.EventTaskSubmitted, bot, taskID, correlation, map[string]any{
		"objective": objective.String(), "idempotencyKey": key.String(),
	}); err != nil {
		return SubmitTaskResult{}, err
	}
	if err := uc.rec.audit(ctx, "task.submit", taskID.String(), domain.AuditOK, map[string]string{"bot": botID.String(), "idempotencyKey": key.String()}); err != nil {
		return SubmitTaskResult{}, err
	}
	return SubmitTaskResult{Task: task, Created: true}, nil
}
