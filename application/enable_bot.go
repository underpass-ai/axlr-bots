package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// EnableBot lets a registered or paused bot accept tasks.
type EnableBot struct {
	deps Dependencies
	rec  recorder
}

// NewEnableBot wires the use case.
func NewEnableBot(deps Dependencies) EnableBot {
	return EnableBot{deps: deps, rec: recorder{deps: deps}}
}

// Execute enables the bot and publishes bot.enabled.
func (uc EnableBot) Execute(ctx context.Context, id domain.BotID) (domain.Bot, error) {
	bot, err := uc.deps.Bots.Get(ctx, id)
	if err != nil {
		return domain.Bot{}, err
	}
	if err := bot.Enable(uc.deps.Clock.Now()); err != nil {
		_ = uc.rec.audit(ctx, "bot.enable", id.String(), domain.AuditDenied, map[string]string{"reason": err.Error()})
		return domain.Bot{}, err
	}
	if err := uc.deps.Bots.Save(ctx, bot); err != nil {
		return domain.Bot{}, err
	}
	if err := uc.rec.event(ctx, domain.EventBotEnabled, bot, "", "", nil); err != nil {
		return domain.Bot{}, err
	}
	if err := uc.rec.audit(ctx, "bot.enable", id.String(), domain.AuditOK, nil); err != nil {
		return domain.Bot{}, err
	}
	return bot, nil
}
