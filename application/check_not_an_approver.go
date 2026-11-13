package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

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
