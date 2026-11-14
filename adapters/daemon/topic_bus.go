package daemon

import (
	"context"

	natsbus "github.com/underpass-ai/axlr-bots/adapters/bus/nats"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
)

// TopicBus carries a team's topics beside the delivery
// (docs/architecture/topics-on-nats.md): Tap decorates the event
// publisher with a copy of every event told to a team, and Listen reads
// the publications an application makes on a team.
type TopicBus interface {
	Tap(inner application.EventPublisher) application.EventPublisher
	Listen(ctx context.Context, service natsbus.PublicationService) error
}

// publications is the service the topic bus drives: the teams and the
// operator's publication through the service the API serves, under the
// runner's lock, and the audit of a refused one.
type publications struct {
	*unixapi.Local
	refuse application.RefusePublication
}

func (p publications) RefusePublication(ctx context.Context, refused application.RefusedPublication) error {
	return p.refuse.Execute(ctx, refused)
}
