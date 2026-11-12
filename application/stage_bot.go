package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// StageBot holds a change to a bot's definition for later: the patch an
// update takes, naming the version it brings. It is checked as the
// update would check it now, on a copy, and nothing of the bot changes
// but what it holds staged: one change at a time, a new one replaces
// it. ApplyStagedBot applies it; UnstageBot drops it. It publishes
// bot.staged with the version, the fields and what it replaced (never
// the patch, which may carry secrets) and the audit log records it
// (bot.stage).
type StageBot struct {
	deps Dependencies
	rec  recorder
}

// NewStageBot wires the use case.
func NewStageBot(deps Dependencies) StageBot {
	return StageBot{deps: deps, rec: recorder{deps: deps}}
}

// Execute stages the patch and returns the bot.
func (uc StageBot) Execute(ctx context.Context, id domain.BotID, patch BotPatch) (domain.Bot, error) {
	bot, err := uc.deps.Bots.Get(ctx, id)
	if err != nil {
		return domain.Bot{}, err
	}
	changed, staged, err := uc.check(ctx, bot, patch)
	if err != nil {
		_ = uc.rec.audit(ctx, "bot.stage", id.String(), domain.AuditDenied, map[string]string{"reason": err.Error()})
		return domain.Bot{}, err
	}
	replaced, hadOne := bot.Staged()
	bot.Stage(staged)
	if err := uc.deps.Bots.Save(ctx, bot); err != nil {
		return domain.Bot{}, err
	}
	// The version and the fields only: the patch may carry secrets.
	details := map[string]string{"version": staged.Version().String(), "fields": joinStrings(changed)}
	payload := map[string]any{"version": staged.Version().String(), "fields": changed}
	if hadOne {
		details["replaced"] = replaced.Version().String()
		payload["replaced"] = replaced.Version().String()
	}
	if err := uc.rec.event(ctx, domain.EventBotStaged, bot, "", "", payload); err != nil {
		return domain.Bot{}, err
	}
	if err := uc.rec.audit(ctx, "bot.stage", id.String(), domain.AuditOK, details); err != nil {
		return domain.Bot{}, err
	}
	return bot, nil
}

// check validates the patch as the update would take it now, on a copy
// of the bot, and makes the staged change of it.
func (uc StageBot) check(ctx context.Context, bot domain.Bot, patch BotPatch) ([]string, domain.StagedDefinition, error) {
	version, err := stagedVersion(patch)
	if err != nil {
		return nil, domain.StagedDefinition{}, err
	}
	// A copy through the snapshot shares nothing with the bot it stages on.
	candidate, err := domain.RestoreBot(bot.Snapshot())
	if err != nil {
		return nil, domain.StagedDefinition{}, err
	}
	changed, err := checkPatch(ctx, uc.deps, &candidate, patch)
	if err != nil {
		return nil, domain.StagedDefinition{}, err
	}
	change, err := encodeStagedPatch(patch)
	if err != nil {
		return nil, domain.StagedDefinition{}, err
	}
	staged, err := domain.NewStagedDefinition(version, uc.deps.Clock.Now(), change)
	if err != nil {
		return nil, domain.StagedDefinition{}, err
	}
	return changed, staged, nil
}
