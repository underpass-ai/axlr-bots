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
// and the control plane. One that hears another team
// (docs/architecture/team-topics.md) names bots of that team only.
func checkSubscriptionSenders(ctx context.Context, deps Dependencies, bot domain.Bot) error {
	mates, err := teammatesOf(ctx, deps, bot)
	if err != nil {
		return err
	}
	ids := botIDs(mates)
	for _, sub := range bot.Subscriptions().List() {
		if sub.Crosses() {
			if err := checkCrossingSenders(ctx, deps, bot, sub); err != nil {
				return err
			}
			continue
		}
		for _, from := range sub.From() {
			if from == domain.FromOperator || from == domain.FromControlPlane || slices.Contains(ids, from) {
				continue
			}
			return domain.ValidationError{Field: "subscriptions", Reason: "subscription " + sub.Topic().String() + " names " + from + " in from, which is not a teammate; its teammates are: " + joinNames(ids)}
		}
	}
	return nil
}

// checkCrossingSenders refuses a from of a subscription to another team
// that names anything but a bot of that team, in the bot's tenant.
func checkCrossingSenders(ctx context.Context, deps Dependencies, bot domain.Bot, sub domain.Subscription) error {
	if len(sub.From()) == 0 {
		return nil
	}
	all, err := deps.Bots.List(ctx)
	if err != nil {
		return err
	}
	var ids []string
	for _, other := range all {
		if other.TenantID() == bot.TenantID() && other.Teams().Has(sub.Team()) {
			ids = append(ids, other.ID().String())
		}
	}
	slices.Sort(ids)
	for _, from := range sub.From() {
		if !slices.Contains(ids, from) {
			return domain.ValidationError{Field: "subscriptions", Reason: "subscription " + sub.Topic().String() + " hears team " + sub.Team().String() + " and names " + from + " in from, which is not a bot of that team; its bots are: " + joinNames(ids)}
		}
	}
	return nil
}
