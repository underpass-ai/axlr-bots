package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// SubmitTask accepts a manual task for an enabled bot. It is idempotent
// on the key: resubmitting the same key returns the same task and starts
// nothing. A task without a correlation id from upstream gets a new
// trace id, which every execution and event of the task then carries.
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
	priority, err := domain.ParseTaskPriority(command.Priority)
	if err != nil {
		return SubmitTaskResult{}, err
	}
	bot, err := uc.deps.Bots.Get(ctx, botID)
	if err != nil {
		return SubmitTaskResult{}, err
	}
	accepts := func() error {
		if !bot.AcceptsTrigger(domain.TriggerManual) {
			_ = uc.rec.audit(ctx, "task.submit", botID.String(), domain.AuditDenied, map[string]string{"reason": "bot takes no manual tasks"})
			return domain.StateError{Subject: "bot " + botID.String(), Current: "without a manual trigger", Attempted: "accept a manual task"}
		}
		return nil
	}
	return submitTask(ctx, uc.deps, submission{action: "task.submit", bot: bot, trigger: domain.ManualTrigger(), objective: objective, key: key, correlation: correlation, accepts: accepts, priority: priority, why: "the operator submitted it as urgent"})
}

// submission is one task to create: what SubmitTask and FireTrigger
// have in common.
type submission struct {
	action      string
	bot         domain.Bot
	trigger     domain.TriggerRef
	objective   domain.Objective
	key         domain.IdempotencyKey
	correlation domain.CorrelationID
	input       []byte
	// lineage links a delegated task to the task that asked for it.
	lineage *domain.TaskLineage
	// accepts refuses the submission for a reason of its own, after the
	// idempotency lookup and the bot's status; nil accepts.
	accepts func() error
	// priority is urgent for a task that goes first and preempts the
	// bot's work in progress; why says who made it urgent.
	priority domain.TaskPriority
	why      string
}

// submitTask creates the task unless the key already names one. The
// task is saved before its event, so a crash leaves a task without an
// event rather than an event without a task.
func submitTask(ctx context.Context, deps Dependencies, s submission) (SubmitTaskResult, error) {
	rec := recorder{deps: deps}
	botID := s.bot.ID()
	if existing, found, err := deps.Tasks.FindByIdempotencyKey(ctx, botID, s.key); err != nil {
		return SubmitTaskResult{}, err
	} else if found {
		if err := rec.audit(ctx, s.action, existing.ID().String(), domain.AuditOK, map[string]string{"idempotencyKey": s.key.String(), "deduplicated": "true"}); err != nil {
			return SubmitTaskResult{}, err
		}
		return SubmitTaskResult{Task: existing, Created: false}, nil
	}
	if !s.bot.AcceptsTasks() {
		_ = rec.audit(ctx, s.action, botID.String(), domain.AuditDenied, map[string]string{"reason": "bot is " + s.bot.Status().String()})
		return SubmitTaskResult{}, domain.StateError{Subject: "bot " + botID.String(), Current: s.bot.Status().String(), Attempted: "accept a task"}
	}
	if s.accepts != nil {
		if err := s.accepts(); err != nil {
			return SubmitTaskResult{}, err
		}
	}
	correlation := s.correlation
	if correlation == "" {
		var err error
		if correlation, err = domain.NewCorrelationID(deps.IDs.NewID("trace-")); err != nil {
			return SubmitTaskResult{}, err
		}
	}
	taskID, err := domain.NewTaskID(deps.IDs.NewID("task-"))
	if err != nil {
		return SubmitTaskResult{}, err
	}
	task, err := domain.NewTask(taskID, botID, s.objective, s.key, correlation, deps.Clock.Now())
	if err != nil {
		return SubmitTaskResult{}, err
	}
	if err := task.Attribute(s.trigger, s.input); err != nil {
		return SubmitTaskResult{}, err
	}
	if s.lineage != nil {
		if err := task.Link(*s.lineage); err != nil {
			return SubmitTaskResult{}, err
		}
	}
	if s.priority == domain.PriorityUrgent {
		task.Prioritize(domain.PriorityUrgent)
	}
	if err := deps.Tasks.Save(ctx, task); err != nil {
		return SubmitTaskResult{}, err
	}
	payload := map[string]any{
		"objective": s.objective.String(), "idempotencyKey": s.key.String(),
		"trigger": map[string]any{"type": s.trigger.Type.String(), "name": s.trigger.Name},
	}
	if task.IsUrgent() {
		payload["priority"], payload["why"] = task.Priority().String(), s.why
	}
	if err := rec.event(ctx, domain.EventTaskSubmitted, s.bot, taskID, correlation, payload); err != nil {
		return SubmitTaskResult{}, err
	}
	details := map[string]string{"bot": botID.String(), "idempotencyKey": s.key.String(), "trigger": s.trigger.Name}
	if err := rec.audit(ctx, s.action, taskID.String(), domain.AuditOK, details); err != nil {
		return SubmitTaskResult{}, err
	}
	if task.IsUrgent() {
		if err := preemptFor(ctx, deps, s.bot, task, s.why); err != nil {
			return SubmitTaskResult{}, err
		}
	}
	return SubmitTaskResult{Task: task, Created: true}, nil
}
