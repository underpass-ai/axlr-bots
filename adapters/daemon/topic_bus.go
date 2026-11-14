package daemon

import (
	"context"

	natsbus "github.com/underpass-ai/axlr-bots/adapters/bus/nats"
	"github.com/underpass-ai/axlr-bots/application"
)

// TopicBus carries a team's topics beside the delivery
// (docs/architecture/topics-on-nats.md): Tap decorates the event
// publisher with a copy of every event told to a team, Listen reads the
// publications an application makes on a team until its context ends,
// and Wait joins that reading.
type TopicBus interface {
	Tap(inner application.EventPublisher) application.EventPublisher
	Listen(ctx context.Context, service natsbus.PublicationService) error
	Wait()
}
