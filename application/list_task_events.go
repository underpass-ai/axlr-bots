package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ListTaskEvents answers a task's timeline: the events about it, oldest
// first, from its submission to its end.
type ListTaskEvents struct {
	deps Dependencies
}

// NewListTaskEvents wires the use case.
func NewListTaskEvents(deps Dependencies) ListTaskEvents { return ListTaskEvents{deps: deps} }

// Execute answers the newest limit events of the task (DefaultEventLimit
// when limit is zero or less, at most MaxEventLimit). A task that does
// not exist is not found.
func (uc ListTaskEvents) Execute(ctx context.Context, id domain.TaskID, limit int) ([]domain.Event, error) {
	return uc.Query(ctx, TaskEventsQuery{ID: id, Limit: limit})
}

// Query answers the events after query.After, oldest first: all of the
// newest limit when After is empty or older than them, as a team's
// timeline does.
func (uc ListTaskEvents) Query(ctx context.Context, query TaskEventsQuery) ([]domain.Event, error) {
	if _, err := uc.deps.Tasks.Get(ctx, query.ID); err != nil {
		return nil, err
	}
	limit := query.Limit
	switch {
	case limit <= 0:
		limit = DefaultEventLimit
	case limit > MaxEventLimit:
		limit = MaxEventLimit
	}
	events, err := uc.deps.EventReader.ListTaskEvents(ctx, query.ID, limit)
	if err != nil {
		return nil, err
	}
	if events == nil {
		events = []domain.Event{}
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
