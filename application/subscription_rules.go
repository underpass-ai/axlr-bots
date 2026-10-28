package application

import (
	"context"
	"reflect"
	"slices"

	"github.com/underpass-ai/axlr-bots/domain"
)

// keepsPendingAsks refuses a list that adds a pending subscription or
// edits one: a bot's ask may only be kept as it is, made active or
// dropped.
func keepsPendingAsks(before, after domain.Subscriptions) error {
	for _, sub := range after.List() {
		if sub.Active() {
			continue
		}
		asked, ok := before.Find(sub.Topic())
		if !ok || asked.Active() || !reflect.DeepEqual(asked.Snapshot(), sub.Snapshot()) {
			return domain.ValidationError{Field: "subscriptions", Reason: "a pending subscription (" + sub.Topic().String() + ") may only be kept as it is, made active or dropped"}
		}
	}
	return nil
}

// checkSubscriptionSenders refuses a from that names a bot outside the
// bot's teams: a subscription hears only its teammates, the operator
// and the control plane.
func checkSubscriptionSenders(ctx context.Context, deps Dependencies, bot domain.Bot) error {
	mates, err := teammatesOf(ctx, deps, bot)
	if err != nil {
		return err
	}
	ids := botIDs(mates)
	for _, sub := range bot.Subscriptions().List() {
		for _, from := range sub.From() {
			if from == domain.FromOperator || from == domain.FromControlPlane || slices.Contains(ids, from) {
				continue
			}
			return domain.ValidationError{Field: "subscriptions", Reason: "subscription " + sub.Topic().String() + " names " + from + " in from, which is not a teammate; its teammates are: " + joinNames(ids)}
		}
	}
	return nil
}
