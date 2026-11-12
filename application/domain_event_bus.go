package application

import "context"

// DomainEventBus is the port a use case publishes another team's domain
// event through (docs/architecture/team-topics.md). Publish hands the
// event over once, for every team of its audience; the adapter carries
// it to the control plane's listener (DomainEventFeed), which delivers
// it with ReceiveDomainEvent. An adapter never blocks the caller for
// long, and never calls back into the runner that holds the caller's
// lock: one that speaks to a broker keeps the event in an outbox.
type DomainEventBus interface {
	Publish(ctx context.Context, event CrossingEvent) error
}
