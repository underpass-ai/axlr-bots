package inprocess

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/bus/bustest"
	"github.com/underpass-ai/axlr-bots/application"
)

func TestTheInProcessBusMeetsTheDomainContract(t *testing.T) {
	bustest.RunDomain(t, func(t *testing.T) (application.DomainEventBus, application.DomainEventFeed) {
		bus := New()
		return bus, bus
	})
}

func TestAPublishWithNobodyListeningIsRefused(t *testing.T) {
	bus := New()
	if err := bus.Publish(context.Background(), bustest.Fact(t, "h-1")); !errors.Is(err, ErrNoListener) {
		t.Errorf("err = %v", err)
	}
	if err := bus.Listen(context.Background(), nil); err == nil {
		t.Error("a listener without a handler")
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := bus.Listen(ctx, func(context.Context, application.CrossingEvent) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := bus.Publish(context.Background(), bustest.Fact(t, "h-2")); err != nil {
		t.Fatal(err)
	}
	cancel()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if err := bus.Publish(context.Background(), bustest.Fact(t, "h-3")); errors.Is(err, ErrNoListener) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the listener outlived its context")
		}
	}
	// What is no domain event of the spec never reaches a listener.
	if err := bus.Listen(context.Background(), func(context.Context, application.CrossingEvent) error { return nil }); err != nil {
		t.Fatal(err)
	}
	bad := bustest.Fact(t, "h-4")
	bad.Crossings = 9
	if err := bus.Publish(context.Background(), bad); err == nil {
		t.Error("a fifth crossing went through")
	}
}
