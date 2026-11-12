package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// UnstageBot drops the change a bot holds staged, unapplied, and audits
// it (bot.unstage). A bot that holds none is a StateError.
type UnstageBot struct {
	deps Dependencies
	rec  recorder
}

// NewUnstageBot wires the use case.
func NewUnstageBot(deps Dependencies) UnstageBot {
	return UnstageBot{deps: deps, rec: recorder{deps: deps}}
}

// Execute drops the staged change and returns the bot.
func (uc UnstageBot) Execute(ctx context.Context, id domain.BotID) (domain.Bot, error) {
	bot, err := uc.deps.Bots.Get(ctx, id)
	if err != nil {
		return domain.Bot{}, err
	}
	staged, ok := bot.Staged()
	if !ok {
		err := nothingStaged(id, "unstage")
		_ = uc.rec.audit(ctx, "bot.unstage", id.String(), domain.AuditDenied, map[string]string{"reason": err.Error()})
		return domain.Bot{}, err
	}
	bot.Unstage()
	if err := uc.deps.Bots.Save(ctx, bot); err != nil {
		return domain.Bot{}, err
	}
	if err := uc.rec.audit(ctx, "bot.unstage", id.String(), domain.AuditOK, map[string]string{"version": staged.Version().String()}); err != nil {
		return domain.Bot{}, err
	}
	return bot, nil
}
