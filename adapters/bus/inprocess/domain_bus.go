// Package inprocess is the domain-event bus within the control plane's
// own process (--bus direct, the default): an event published is handed
// to the listener at once, on the publisher's own call, after it went
// through the wire form (domainevent.Encode and Decode), so the direct
// bus reads the spec's DTO as a broker's consumer does
// (docs/architecture/team-topics.md).
//
// The publisher is EmitEvent under the runner's lock, so the listener
// must be the bare ReceiveDomainEvent over the same dependencies, never
// a runner method that takes the lock again. Nothing is kept: a handler
// that fails fails the publish, and the node's emit is retried whole
// (the receiver's key keeps the fact one task).
package inprocess

import (
	"context"
	"errors"
	"sync"

	"github.com/underpass-ai/axlr-bots/adapters/bus/domainevent"
	"github.com/underpass-ai/axlr-bots/application"
)

// ErrNoListener is a publish before anyone listens.
var ErrNoListener = errors.New("inprocess: nobody listens for domain events yet")

// DomainBus is application.DomainEventBus and application.DomainEventFeed
// in one process.
type DomainBus struct {
	mu     sync.Mutex
	handle application.DomainEventHandler
}

var (
	_ application.DomainEventBus  = (*DomainBus)(nil)
	_ application.DomainEventFeed = (*DomainBus)(nil)
)

// New makes a bus nobody listens on yet.
func New() *DomainBus { return &DomainBus{} }

// Publish hands the event, through its wire form, to the listener and
// answers the listener's error.
func (b *DomainBus) Publish(ctx context.Context, event application.CrossingEvent) error {
	b.mu.Lock()
	handle := b.handle
	b.mu.Unlock()
	if handle == nil {
		return ErrNoListener
	}
	message, err := domainevent.Encode(event)
	if err != nil {
		return err
	}
	read, err := domainevent.Decode(message.Payload)
	if err != nil {
		return err
	}
	return handle(ctx, read)
}

// Listen hands every event published from now on to the handler, until
// ctx ends.
func (b *DomainBus) Listen(ctx context.Context, handle application.DomainEventHandler) error {
	if handle == nil {
		return errors.New("inprocess: a listener needs a handler")
	}
	b.mu.Lock()
	b.handle = handle
	b.mu.Unlock()
	go func() {
		<-ctx.Done()
		b.mu.Lock()
		defer b.mu.Unlock()
		b.handle = nil
	}()
	return nil
}
