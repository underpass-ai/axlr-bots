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
	if _, err := uc.deps.Tasks.Get(ctx, id); err != nil {
		return nil, err
	}
	switch {
	case limit <= 0:
		limit = DefaultEventLimit
	case limit > MaxEventLimit:
		limit = MaxEventLimit
	}
	events, err := uc.deps.EventReader.ListTaskEvents(ctx, id, limit)
	if err != nil {
		return nil, err
	}
	if events == nil {
		events = []domain.Event{}
	}
	return events, nil
}
