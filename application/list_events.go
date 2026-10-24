package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// Bounds of an event listing.
const (
	DefaultEventLimit = 100
	MaxEventLimit     = 1000
)

// ListEvents reads the newest events of a bot, oldest first.
type ListEvents struct {
	deps Dependencies
}

// NewListEvents wires the use case.
func NewListEvents(deps Dependencies) ListEvents {
	return ListEvents{deps: deps}
}

// Execute returns up to limit events (DefaultEventLimit when zero or
// less, MaxEventLimit at most) of the bot, which must exist.
func (uc ListEvents) Execute(ctx context.Context, id domain.BotID, limit int) ([]domain.Event, error) {
	if _, err := uc.deps.Bots.Get(ctx, id); err != nil {
		return nil, err
	}
	switch {
	case limit <= 0:
		limit = DefaultEventLimit
	case limit > MaxEventLimit:
		limit = MaxEventLimit
	}
	events, err := uc.deps.EventReader.ListEvents(ctx, id, limit)
	if err != nil {
		return nil, err
	}
	if events == nil {
		events = []domain.Event{}
	}
	return events, nil
}
