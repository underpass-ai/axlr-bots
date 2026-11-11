package application

import "context"

// DomainEventHandler takes one domain event off the bus; an error asks
// the bus to give it again.
type DomainEventHandler func(ctx context.Context, event CrossingEvent) error
