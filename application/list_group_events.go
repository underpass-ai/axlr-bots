package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// Limits of a team's timeline.
const (
	DefaultGroupEvents = 100
	MaxGroupEvents     = 1000
)

// ListGroupEvents reads a team's timeline: every event of its members
// and the operator's events on it, in the order they were published,
// after the caller's last one.
type ListGroupEvents struct {
	deps Dependencies
}

// NewListGroupEvents wires the use case.
func NewListGroupEvents(deps Dependencies) ListGroupEvents { return ListGroupEvents{deps: deps} }

// Execute answers the events after query.After, oldest first: all of the
// newest limit when After is empty or older than them.
func (uc ListGroupEvents) Execute(ctx context.Context, query GroupEventsQuery) ([]domain.Event, error) {
	limit := query.Limit
	switch {
	case limit < 0:
		return nil, domain.ValidationError{Field: "limit", Reason: "must not be negative"}
	case limit == 0:
		limit = DefaultGroupEvents
	case limit > MaxGroupEvents:
		limit = MaxGroupEvents
	}
	group, err := NewGetGroup(uc.deps).Execute(ctx, query.Name, query.Tenant)
	if err != nil {
		return nil, err
	}
	bots := make([]domain.BotID, len(group.Members))
	for i, member := range group.Members {
		bots[i] = domain.BotID(member.BotID)
	}
	events, err := uc.deps.EventReader.ListTeamEvents(ctx, domain.TenantID(group.TenantID), domain.TeamName(group.Name), bots, limit)
	if err != nil {
		return nil, err
	}
	if query.After == "" {
		return events, nil
	}
	for i, event := range events {
		if event.ID.String() == query.After {
			return events[i+1:], nil
		}
	}
	return events, nil
}
