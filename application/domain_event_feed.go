package application

import "context"

// DomainEventFeed is the listening end of the domain-event bus: Listen
// hands each event published to the handler, until ctx ends. An event
// whose handler fails is not lost: the adapter gives it again, or
// answers the failure to the publisher. The same event may come twice;
// ReceiveDomainEvent makes one task of it.
type DomainEventFeed interface {
	Listen(ctx context.Context, handle DomainEventHandler) error
}
