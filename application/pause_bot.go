package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// PauseBot puts an enabled bot on hold: it accepts no task, from an
// operator or a trigger, until it is enabled again. Its tasks, its
// events and its timers stay.
type PauseBot struct {
	deps Dependencies
	rec  recorder
}

// NewPauseBot wires the use case.
func NewPauseBot(deps Dependencies) PauseBot {
	return PauseBot{deps: deps, rec: recorder{deps: deps}}
}

// Execute pauses the bot, publishes bot.paused and audits the act; a
// bot that is not Enabled is refused as a state error.
func (uc PauseBot) Execute(ctx context.Context, id domain.BotID) (domain.Bot, error) {
	bot, err := uc.deps.Bots.Get(ctx, id)
	if err != nil {
		return domain.Bot{}, err
	}
	if err := bot.Pause(uc.deps.Clock.Now()); err != nil {
		_ = uc.rec.audit(ctx, "bot.pause", id.String(), domain.AuditDenied, map[string]string{"reason": err.Error()})
		return domain.Bot{}, err
	}
	if err := uc.deps.Bots.Save(ctx, bot); err != nil {
		return domain.Bot{}, err
	}
	if err := uc.rec.event(ctx, domain.EventBotPaused, bot, "", "", nil); err != nil {
		return domain.Bot{}, err
	}
	if err := uc.rec.audit(ctx, "bot.pause", id.String(), domain.AuditOK, nil); err != nil {
		return domain.Bot{}, err
	}
	return bot, nil
}
