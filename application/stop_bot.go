package application

import (
	"context"
	"strconv"

	"github.com/underpass-ai/axlr-bots/domain"
)

// StopBot stops a bot at once: it pauses the bot when it is Enabled, so
// nothing new reaches it, and cancels every task it has not finished,
// Pending, Running or WaitingApproval, with the reason who stopped it. A
// Running task's node stops its run at the next lease renewal; a waiting
// call never gets its decision. Enable undoes the pause.
type StopBot struct {
	deps Dependencies
	rec  recorder
}

// NewStopBot wires the use case.
func NewStopBot(deps Dependencies) StopBot {
	return StopBot{deps: deps, rec: recorder{deps: deps}}
}

// Execute stops the bot and answers what it cancelled.
func (uc StopBot) Execute(ctx context.Context, id domain.BotID) (StopBotResult, error) {
	bot, err := uc.deps.Bots.Get(ctx, id)
	if err != nil {
		return StopBotResult{}, err
	}
	if bot.Status() == domain.BotEnabled {
		if bot, err = NewPauseBot(uc.deps).Execute(ctx, id); err != nil {
			return StopBotResult{}, err
		}
	}
	tasks, err := uc.deps.Tasks.List(ctx)
	if err != nil {
		return StopBotResult{}, err
	}
	result := StopBotResult{Bot: bot}
	reason := "stopped by " + uc.deps.Actor
	for _, task := range tasks {
		if task.BotID() != id || task.Status().IsTerminal() {
			continue
		}
		if _, err := stopTask(ctx, uc.deps, bot, task, reason, domain.EventTaskCancelled, "task.cancel", map[string]any{"stopped": true}); err != nil {
			result.NotCancelled = append(result.NotCancelled, StopRefusal{TaskID: task.ID().String(), Reason: err.Error()})
			continue
		}
		result.Cancelled = append(result.Cancelled, task.ID())
	}
	details := map[string]string{"cancelled": strconv.Itoa(len(result.Cancelled)), "notCancelled": strconv.Itoa(len(result.NotCancelled))}
	if err := uc.rec.audit(ctx, "bot.stop", id.String(), domain.AuditOK, details); err != nil {
		return StopBotResult{}, err
	}
	return result, nil
}
