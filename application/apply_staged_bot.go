package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ApplyStagedBot applies the change a bot holds staged through the
// update, so it is checked, published (bot.updated, its payload naming
// the staged version) and audited as any update, and drops it in the
// same save; then bot.staged.apply in the audit log. A bot
// that holds none is a StateError; a change the bot no longer takes
// (it changed since) is refused as the update refuses it, and stays
// staged.
type ApplyStagedBot struct {
	deps Dependencies
	rec  recorder
}

// NewApplyStagedBot wires the use case.
func NewApplyStagedBot(deps Dependencies) ApplyStagedBot {
	return ApplyStagedBot{deps: deps, rec: recorder{deps: deps}}
}

// Execute applies the staged change and returns the bot.
func (uc ApplyStagedBot) Execute(ctx context.Context, id domain.BotID) (domain.Bot, error) {
	bot, err := uc.deps.Bots.Get(ctx, id)
	if err != nil {
		return domain.Bot{}, err
	}
	staged, ok := bot.Staged()
	if !ok {
		err := nothingStaged(id, "apply a staged change")
		_ = uc.rec.audit(ctx, "bot.staged.apply", id.String(), domain.AuditDenied, map[string]string{"reason": err.Error()})
		return domain.Bot{}, err
	}
	patch, err := decodeStagedPatch(staged)
	if err != nil {
		_ = uc.rec.audit(ctx, "bot.staged.apply", id.String(), domain.AuditDenied, map[string]string{"reason": err.Error()})
		return domain.Bot{}, err
	}
	note := map[string]any{"staged": staged.Version().String()}
	updated, err := NewUpdateBot(uc.deps).update(ctx, bot, patch, func(b *domain.Bot) { b.Unstage() }, note)
	if err != nil {
		_ = uc.rec.audit(ctx, "bot.staged.apply", id.String(), domain.AuditDenied, map[string]string{"version": staged.Version().String(), "reason": err.Error()})
		return domain.Bot{}, err
	}
	if err := uc.rec.audit(ctx, "bot.staged.apply", id.String(), domain.AuditOK, map[string]string{"version": staged.Version().String()}); err != nil {
		return domain.Bot{}, err
	}
	return updated, nil
}
