package daemon

import "github.com/underpass-ai/axlr-bots/application"

// DomainBus is a domain-event bus adapter: where the emits publish and
// what the control plane listens on.
type DomainBus interface {
	application.DomainEventBus
	application.DomainEventFeed
}
