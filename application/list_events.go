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
// less, MaxEventLimit at most) of the bot. A deleted bot's history stays
// readable, bot.deleted last: only an id that is neither a bot nor in
// the log is not found.
func (uc ListEvents) Execute(ctx context.Context, id domain.BotID, limit int) ([]domain.Event, error) {
	_, missing := uc.deps.Bots.Get(ctx, id)
	if missing != nil && !domain.IsNotFound(missing) {
		return nil, missing
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
	if len(events) == 0 && missing != nil {
		return nil, missing
	}
	if events == nil {
		events = []domain.Event{}
	}
	return events, nil
}
