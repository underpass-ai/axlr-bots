package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// EventReader reads back the events a bot's work produced, for the
// operator: the newest limit events, oldest first.
type EventReader interface {
	ListEvents(ctx context.Context, bot domain.BotID, limit int) ([]domain.Event, error)
}
