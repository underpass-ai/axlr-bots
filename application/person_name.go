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

// checkNotAnApprover refuses a bot id that is an approver any bot names:
// a registered bot is no person, so the person would be locked out
// without a word.
func checkNotAnApprover(ctx context.Context, deps Dependencies, id domain.BotID) error {
	bots, err := deps.Bots.List(ctx)
	if err != nil {
		return err
	}
	key := domain.ActorKey(id.String())
	for _, bot := range bots {
		for _, approver := range bot.Approvals().Approvers() {
			if domain.ActorKey(approver) == key {
				return domain.ValidationError{Field: "id", Reason: id.String() + " is an approver of bot " + bot.ID().String() + ": a bot may not take a person's name"}
			}
		}
	}
	return nil
}
