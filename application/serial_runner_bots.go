package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// The bot writers go through the runner too, under mu: each reads the
// bot, changes it and saves it, and a bot's subscription asked for by its
// own run (Subscribe) does the same under mu. Without the lock one of two
// writes at once was lost (QA L12, 5 November 2026), and a delete could
// race a task being assigned or a timer firing. None reaches the
// runtime, so none takes the slot.

// RegisterBot registers a bot, as RegisterBot does.
func (r *SerialRunner) RegisterBot(ctx context.Context, definition BotDefinition) (domain.Bot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewRegisterBot(r.deps).Execute(ctx, definition)
}

// UpdateBot applies a patch to a bot, as UpdateBot does.
func (r *SerialRunner) UpdateBot(ctx context.Context, id domain.BotID, patch BotPatch) (domain.Bot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewUpdateBot(r.deps).Execute(ctx, id, patch)
}

// EnableBot enables a bot, as EnableBot does.
func (r *SerialRunner) EnableBot(ctx context.Context, id domain.BotID) (domain.Bot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewEnableBot(r.deps).Execute(ctx, id)
}

// PauseBot pauses a bot, as PauseBot does.
func (r *SerialRunner) PauseBot(ctx context.Context, id domain.BotID) (domain.Bot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewPauseBot(r.deps).Execute(ctx, id)
}

// StopBot stops a bot, as StopBot does; a run in process is interrupted
// by the cancel events it publishes.
func (r *SerialRunner) StopBot(ctx context.Context, id domain.BotID) (StopBotResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewStopBot(r.deps).Execute(ctx, id)
}

// DeleteBot deletes a bot, as DeleteBot does, under the same lock as
// the assignments and the reports: no task of it is handed to a node,
// settled or delegated to while it goes.
func (r *SerialRunner) DeleteBot(ctx context.Context, id domain.BotID) (DeleteBotResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewDeleteBot(r.deps).Execute(ctx, id)
}

// TickTimers runs the scheduler's pass, as TickTimers does: a timer
// never fires for a bot a delete is removing.
func (r *SerialRunner) TickTimers(ctx context.Context) (TickTimersResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewTickTimers(r.deps).Execute(ctx)
}

// StageBot stages a change to a bot, as StageBot does.
func (r *SerialRunner) StageBot(ctx context.Context, id domain.BotID, patch BotPatch) (domain.Bot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewStageBot(r.deps).Execute(ctx, id, patch)
}

// ApplyStagedBot applies a bot's staged change, as ApplyStagedBot does.
func (r *SerialRunner) ApplyStagedBot(ctx context.Context, id domain.BotID) (domain.Bot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewApplyStagedBot(r.deps).Execute(ctx, id)
}

// UnstageBot drops a bot's staged change, as UnstageBot does.
func (r *SerialRunner) UnstageBot(ctx context.Context, id domain.BotID) (domain.Bot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewUnstageBot(r.deps).Execute(ctx, id)
}
