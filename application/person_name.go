package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// personName reports a validated actor name that may be a person's: no
// machine's prefix, not a registered bot's id and, on a daemon, not the
// control plane's own. A store that cannot say counts as no person.
func personName(ctx context.Context, deps Dependencies, name string) bool {
	if domain.IsMachineActor(name) {
		return false
	}
	if deps.Daemon && domain.ActorKey(name) == domain.ActorKey(deps.Actor) {
		return false
	}
	return !isRegisteredBot(ctx, deps, name)
}

// isRegisteredBot reports a name that is a bot's id, compared through
// ActorKey.
func isRegisteredBot(ctx context.Context, deps Dependencies, name string) bool {
	id, err := domain.NewBotID(domain.ActorKey(name))
	if err != nil {
		return false
	}
	_, err = deps.Bots.Get(ctx, id)
	return !domain.IsNotFound(err)
}

// checkLadderApprovers refuses a bot on a ladder that lists a registered
// bot as a person.
func checkLadderApprovers(ctx context.Context, deps Dependencies, bot domain.Bot) error {
	trigger, ok := bot.Triggers().Incident(domain.IncidentPhaseApply)
	if !ok {
		return nil
	}
	if _, ok := trigger.Ladder(); !ok {
		return nil
	}
	for _, approver := range bot.Approvals().Approvers() {
		if isRegisteredBot(ctx, deps, approver) {
			return domain.ValidationError{Field: "approvals approvers", Reason: approver + " is a registered bot: a ladder counts people"}
		}
	}
	return nil
}
