package domain

import "time"

// DomainEvent is a fact of the business that one team publishes to
// another: its name (`<thing>.<what happened>`, of the info kind), the
// id of the thing, the KMP key of its context when it has one, the team
// that publishes it, the bot that did and when it happened. It carries no instruction:
// the listener reads the rest from its own programs, and asks the
// emitting team for the context (docs/architecture/team-topics.md).
// The wire form is a DTO of the adapters, never this type.
type DomainEvent struct {
	name    EventName
	id      DomainID
	context ContextKey
	team    TeamName
	from    BotID
	at      time.Time
}

// NewDomainEvent validates a domain event: an info name (never a control
// signal), the thing's id, an optional context key, the team and the bot
// that publish it, and when it happened.
func NewDomainEvent(name EventName, id DomainID, context ContextKey, team TeamName, from BotID, at time.Time) (DomainEvent, error) {
	if _, err := NewEventName(name.String()); err != nil {
		return DomainEvent{}, invalid("domain event name", err.Error())
	}
	if KindOf(name) != KindInfo {
		return DomainEvent{}, invalid("domain event name", "a domain event is an info name, never stop, danger, warn, error or done")
	}
	if _, err := NewDomainID(id.String()); err != nil {
		return DomainEvent{}, err
	}
	if context != "" {
		if _, err := NewContextKey(context.String()); err != nil {
			return DomainEvent{}, err
		}
	}
	if _, err := NewTeamName(team.String()); err != nil {
		return DomainEvent{}, invalid("domain event team", err.Error())
	}
	if _, err := NewBotID(from.String()); err != nil {
		return DomainEvent{}, invalid("domain event from", err.Error())
	}
	if at.IsZero() {
		return DomainEvent{}, invalid("domain event at", "says when it happened")
	}
	return DomainEvent{name: name, id: id, context: context, team: team, from: from, at: at.UTC()}, nil
}

// Name is what happened.
func (e DomainEvent) Name() EventName { return e.name }

// ID is the id of the thing it happened to.
func (e DomainEvent) ID() DomainID { return e.id }

// Context is the KMP key of the event's context; empty when it has none.
func (e DomainEvent) Context() ContextKey { return e.context }

// Team is the team that publishes it.
func (e DomainEvent) Team() TeamName { return e.team }

// From is the bot that published it.
func (e DomainEvent) From() BotID { return e.from }

// At is when it happened, in UTC.
func (e DomainEvent) At() time.Time { return e.at }

// Identity is the event's id: the same fact (team, name, id) published
// twice, by any member of the team, is one event.
func (e DomainEvent) Identity() string { return e.id.EventIdentity(e.team, e.name) }
