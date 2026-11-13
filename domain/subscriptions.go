package domain

import (
	"strings"
	"time"
)

// MaxSubscriptions is the most topics a bot may listen to.
const MaxSubscriptions = 16

// Subscriptions are the topics a bot listens to in its teams, one per
// pattern, at most 16.
type Subscriptions struct {
	list []Subscription
}

// NewSubscriptions validates the list and refuses a repeated pattern.
func NewSubscriptions(definitions []SubscriptionSnapshot) (Subscriptions, error) {
	if len(definitions) > MaxSubscriptions {
		return Subscriptions{}, invalid("subscriptions", "names at most 16 topics")
	}
	out := Subscriptions{}
	seen := map[EventPattern]bool{}
	for _, definition := range definitions {
		sub, err := NewSubscription(definition)
		if err != nil {
			return Subscriptions{}, err
		}
		if seen[sub.Topic()] {
			return Subscriptions{}, invalid("subscriptions", "repeat the topic "+sub.Topic().String())
		}
		seen[sub.Topic()] = true
		out.list = append(out.list, sub)
	}
	return out, nil
}

// List returns the subscriptions, copied.
func (s Subscriptions) List() []Subscription { return append([]Subscription(nil), s.list...) }

// Find finds the subscription of the pattern.
func (s Subscriptions) Find(topic EventPattern) (Subscription, bool) {
	for _, sub := range s.list {
		if sub.Topic() == topic {
			return sub, true
		}
	}
	return Subscription{}, false
}

// HasActive reports whether any subscription hears.
func (s Subscriptions) HasActive() bool {
	for _, sub := range s.list {
		if sub.Active() {
			return true
		}
	}
	return false
}

// Hearing finds the bot's way of hearing the event: the first active
// subscription whose topic and from fit; else, when the event is
// addressed to the bot or is the team's default (danger, stop: every
// bot hears them, whatever it subscribes to, so a Stop always lands),
// the implicit subscription. False: the bot does not hear it.
func (s Subscriptions) Hearing(name EventName, from string, by Publisher, addressed bool) (Subscription, bool) {
	for _, sub := range s.list {
		if sub.Matches(name, from, by) {
			return sub, true
		}
	}
	if addressed || teamDefault(name) {
		return implicitSubscription(name), true
	}
	return Subscription{}, false
}

// teamDefault reports whether every bot of the team hears the event
// without subscribing: danger and stop.
func teamDefault(name EventName) bool {
	kind := KindOf(name)
	return kind == KindDanger || kind == KindStop
}

// Snapshot returns the subscriptions as plain data, nil when there are
// none.
func (s Subscriptions) Snapshot() []SubscriptionSnapshot {
	if len(s.list) == 0 {
		return nil
	}
	out := make([]SubscriptionSnapshot, 0, len(s.list))
	for _, sub := range s.list {
		out = append(out, sub.Snapshot())
	}
	return out
}

// SubscriptionsFromTriggers reads a definition's event triggers as
// subscriptions, for one release: one subscription of mode task per
// pattern, interrupt as urgent, the objective kept. It answers the other
// triggers and the subscriptions, with those given first and a pattern
// they already name left out.
func SubscriptionsFromTriggers(triggers []TriggerSnapshot, given []SubscriptionSnapshot) ([]TriggerSnapshot, []SubscriptionSnapshot) {
	var rest []TriggerSnapshot
	subs := append([]SubscriptionSnapshot(nil), given...)
	named := map[string]bool{}
	for _, sub := range given {
		named[sub.Topic] = true
	}
	for _, trigger := range triggers {
		if trigger.Type != TriggerEvent.String() {
			rest = append(rest, trigger)
			continue
		}
		for _, pattern := range trigger.Events {
			if named[pattern] {
				continue
			}
			named[pattern] = true
			subs = append(subs, SubscriptionSnapshot{Topic: pattern, Mode: ModeTask.String(), Urgent: trigger.Interrupt, Objective: trigger.Objective})
		}
	}
	return rest, subs
}

// Reasons a bot's ask for a subscription is refused.
const (
	// RefusedAlreadySubscribed: the bot listens to the pattern, or asked
	// for it already.
	RefusedAlreadySubscribed = "already_subscribed"
	// RefusedTooMany: the bot listens to 16 topics.
	RefusedTooMany = "too_many"
)

// Ask adds the subscription a bot asks for, pending until a person
// approves it; it answers why not when the bot has the pattern already
// or has 16.
func (s Subscriptions) Ask(topic string, bot BotID, why string, at time.Time) (Subscriptions, string, error) {
	at = at.UTC()
	asked, err := NewSubscription(SubscriptionSnapshot{Topic: topic, Status: SubscriptionPending.String(), AskedBy: bot.String(), Why: why, AskedAt: &at})
	if err != nil {
		return Subscriptions{}, "", err
	}
	if strings.TrimSpace(why) == "" {
		return Subscriptions{}, "", invalid("why", "must not be empty")
	}
	if _, found := s.Find(asked.Topic()); found {
		return s, RefusedAlreadySubscribed, nil
	}
	if len(s.list) >= MaxSubscriptions {
		return s, RefusedTooMany, nil
	}
	return Subscriptions{list: append(s.List(), asked)}, "", nil
}
