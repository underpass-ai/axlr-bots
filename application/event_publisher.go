package application

import (
	"context"

	"github.com/underpass-ai/underpass-bots/domain"
)

// EventPublisher hands the control plane's events to whoever keeps or
// routes them: a log, a bus.
type EventPublisher interface {
	Publish(ctx context.Context, event domain.Event) error
}
