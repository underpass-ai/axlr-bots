package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// autonomyActor is who may change a runbook's record by hand: a named
// person among the apply bot's approvers.
func autonomyActor(ctx context.Context, deps Dependencies, autonomy domain.RunbookAutonomy, actor string) (string, error) {
	name, err := domain.ValidateActor("actor", actor)
	if err != nil {
		return "", domain.ForbiddenError{Reason: "a runbook is changed by a named person: name who does it (--actor or X-Axlr-Actor)"}
	}
	if !personName(ctx, deps, name) {
		return "", domain.ForbiddenError{Reason: name + " is not a person's name"}
	}
	bot, err := deps.Bots.Get(ctx, autonomy.BotID())
	if err != nil && !domain.IsNotFound(err) {
		return "", err
	}
	if err == nil && !bot.Approvals().MayDecide(name) {
		return "", domain.ForbiddenError{Reason: name + " is not among the approvers of bot " + bot.ID().String()}
	}
	return name, nil
}

func refuseAutonomy(ctx context.Context, rec recorder, action string, id domain.AutonomyID, actor string, cause error) error {
	_ = rec.audit(ctx, action, id.String(), domain.AuditDenied, map[string]string{"actor": actor, "reason": cause.Error()})
	return cause
}
