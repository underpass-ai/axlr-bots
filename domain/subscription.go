package domain

import (
	"slices"
	"strings"
	"time"
)

// Senders a subscription's from may name besides a bot.
const (
	// FromOperator is a person publishing through the operator API.
	FromOperator = "operator"
	// FromControlPlane is the control plane telling a fact of its own.
	FromControlPlane = "control-plane"
)

// Limits of a subscription.
const (
	maxSubscriptionFrom = 16
	// MaxSubscriptionWhy bounds why a bot asks for a subscription.
	MaxSubscriptionWhy = 280
)

// Subscription is one topic a bot listens to in its teams: a name or
// pattern, what a heard event does (mode), whom from, whether a warning
// is urgent, the objective of the task it makes, whether its team takes
// what it hears and whether a person approved it. The implicit subscription is the one a bot hears an
// event by when the event is addressed to it or is the team's default
// (danger, stop) and nothing it subscribed to fits.
type Subscription struct {
	topic     EventPattern
	team      TeamName
	mode      DeliveryMode
	from      []string
	urgent    bool
	objective string
	deliver   DeliveryScope
	status    SubscriptionStatus
	askedBy   string
	why       string
	askedAt   time.Time
	implicit  bool
}

// NewSubscription validates a subscription.
func NewSubscription(s SubscriptionSnapshot) (Subscription, error) {
	topic, err := NewEventPattern(s.Topic)
	if err != nil {
		return Subscription{}, invalid("subscription "+s.Topic, err.Error())
	}
	field := "subscription " + topic.String()
	mode, err := ParseDeliveryMode(s.Mode)
	if err != nil {
		return Subscription{}, invalid(field, err.Error())
	}
	status, err := ParseSubscriptionStatus(s.Status)
	if err != nil {
		return Subscription{}, invalid(field, err.Error())
	}
	if len(s.From) > maxSubscriptionFrom {
		return Subscription{}, invalid(field+" from", "names at most 16 senders")
	}
	deliver, err := ParseDeliveryScope(s.Deliver)
	if err != nil {
		return Subscription{}, invalid(field, err.Error())
	}
	if deliver.Team() && mode != ModeTask && s.Team == "" {
		// Another team's domain event is a task even when mode is absent.
		return Subscription{}, invalid(field+" deliver", "team needs mode task: one member takes the event as a task")
	}
	if deliver.Team() && status == SubscriptionPending {
		return Subscription{}, invalid(field+" deliver", "a subscription a bot asks for is its own, never the team's")
	}
	sub := Subscription{topic: topic, mode: mode, urgent: s.Urgent, deliver: deliver, status: status}
	if s.Team != "" {
		if sub.team, err = crossingTeam(field, s, mode, status); err != nil {
			return Subscription{}, err
		}
	}
	for _, raw := range s.From {
		from := strings.TrimSpace(raw)
		if from != FromOperator && from != FromControlPlane {
			id, err := NewBotID(from)
			if err != nil {
				return Subscription{}, invalid(field+" from", "names bots, operator or control-plane: "+err.Error())
			}
			from = id.String()
		}
		if !slices.Contains(sub.from, from) {
			sub.from = append(sub.from, from)
		}
	}
	slices.Sort(sub.from)
	if objective := strings.TrimSpace(s.Objective); objective != "" {
		if err := ValidateObjectiveTemplate(objective); err != nil {
			return Subscription{}, invalid(field+" objective", err.Error())
		}
		sub.objective = objective
	}
	if s.AskedBy != "" {
		id, err := NewBotID(s.AskedBy)
		if err != nil {
			return Subscription{}, invalid(field+" askedBy", err.Error())
		}
		sub.askedBy = id.String()
	}
	if why := strings.TrimSpace(s.Why); why != "" {
		if sub.why, err = validateText(field+" why", why, MaxSubscriptionWhy); err != nil {
			return Subscription{}, err
		}
	}
	if s.AskedAt != nil {
		sub.askedAt = s.AskedAt.UTC()
	}
	return sub, nil
}

// crossingTeam validates the other team a subscription hears from: its
// events reach the bot as tasks of its own, never as a message or a note,
// never urgent, and never by a bot's own ask.
func crossingTeam(field string, s SubscriptionSnapshot, mode DeliveryMode, status SubscriptionStatus) (TeamName, error) {
	team, err := NewTeamName(s.Team)
	if err != nil {
		return "", invalid(field+" team", err.Error())
	}
	if mode != "" && mode != ModeTask {
		return "", invalid(field+" team", "another team's domain event is a task: mode task or absent")
	}
	if s.Urgent {
		return "", invalid(field+" team", "another team's domain event is never urgent")
	}
	if status == SubscriptionPending {
		return "", invalid(field+" team", "a subscription a bot asks for hears its own teams only")
	}
	return team, nil
}

// implicitSubscription is how a bot hears what is addressed to it, or the
// team's default, when nothing it subscribed to fits.
func implicitSubscription(name EventName) Subscription {
	return Subscription{topic: EventPattern(name), status: SubscriptionActive, implicit: true}
}

// Topic is the name or pattern the subscription listens to.
func (s Subscription) Topic() EventPattern { return s.topic }

// Team is the other team the subscription hears its topic from; empty:
// the bot's own teams.
func (s Subscription) Team() TeamName { return s.team }

// Crosses reports whether the subscription hears another team.
func (s Subscription) Crosses() bool { return s.team != "" }

// Mode is the mode the subscription names; empty: by the severity.
func (s Subscription) Mode() DeliveryMode { return s.mode }

// ModeFor is what an event of the severity heard by the subscription
// does: its own mode, or the severity's default.
func (s Subscription) ModeFor(severity Severity) DeliveryMode {
	if s.Crosses() {
		return ModeTask
	}
	if s.mode != "" {
		return s.mode
	}
	return DefaultMode(severity)
}

// From lists whom the subscription hears; empty: anyone in the team.
func (s Subscription) From() []string { return append([]string(nil), s.from...) }

// Allows reports whether the subscription hears an event of the sender
// (a bot's id) published by a bot, the operator or the control plane.
func (s Subscription) Allows(from string, by Publisher) bool {
	if len(s.from) == 0 {
		return true
	}
	switch by {
	case PublisherOperator:
		return slices.Contains(s.from, FromOperator)
	case PublisherControlPlane:
		if slices.Contains(s.from, FromControlPlane) {
			return true
		}
	}
	return slices.Contains(s.from, from)
}

// Urgent reports whether a warning the subscription hears makes an
// urgent task.
func (s Subscription) Urgent() bool { return s.urgent }

// ObjectiveTemplate is the template of the task the subscription makes;
// empty: DefaultEventObjective.
func (s Subscription) ObjectiveTemplate() string { return s.objective }

// Deliver says whether the bot or one member of its team takes what the
// subscription hears.
func (s Subscription) Deliver() DeliveryScope { return s.deliver }

// Status says whether the subscription hears.
func (s Subscription) Status() SubscriptionStatus { return s.status }

// Active reports whether the subscription hears.
func (s Subscription) Active() bool { return s.status != SubscriptionPending }

// AskedBy is the bot that asked for the subscription; empty for the
// operator's own.
func (s Subscription) AskedBy() string { return s.askedBy }

// Implicit reports whether the bot heard the event because it was
// addressed to it or was the team's default, not by a subscription.
func (s Subscription) Implicit() bool { return s.implicit }

// TriggerName names the subscription on the task it makes: its topic, or
// team for the implicit one.
func (s Subscription) TriggerName() string {
	if s.implicit {
		return TeamEventTrigger().Name()
	}
	return s.topic.String()
}

// Matches reports whether an active subscription takes the name from
// the sender in the bot's own teams; one that hears another team never
// does.
func (s Subscription) Matches(name EventName, from string, by Publisher) bool {
	return !s.Crosses() && s.Active() && s.topic.Matches(name) && s.Allows(from, by)
}

// MatchesFrom reports whether an active subscription takes the domain
// event a bot of the other team published.
func (s Subscription) MatchesFrom(team TeamName, name EventName, from string) bool {
	return s.Crosses() && s.team == team && s.Active() && s.topic.Matches(name) && s.Allows(from, PublisherBot)
}

// Snapshot returns the subscription as plain data.
func (s Subscription) Snapshot() SubscriptionSnapshot {
	out := SubscriptionSnapshot{Topic: s.topic.String(), Team: s.team.String(), Mode: s.mode.String(), From: s.From(), Urgent: s.urgent, Objective: s.objective, Deliver: s.deliver.String(), AskedBy: s.askedBy, Why: s.why}
	if s.status == SubscriptionPending {
		out.Status = s.status.String()
	}
	if !s.askedAt.IsZero() {
		at := s.askedAt
		out.AskedAt = &at
	}
	return out
}
