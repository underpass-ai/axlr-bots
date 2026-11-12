package bustest

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// DomainFactory builds one domain-event bus adapter's two ends: the bus
// the emit publishes through and the feed the control plane listens on.
type DomainFactory func(t *testing.T) (application.DomainEventBus, application.DomainEventFeed)

// Fact is the domain event the contract publishes: team-b's finding id, to team-a.
func Fact(t *testing.T, id domain.DomainID) application.CrossingEvent {
	t.Helper()
	event, err := domain.NewDomainEvent("order.new", id, domain.ContextKey("project:team-b.item."+id.String()), "team-b", "b-triage", time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	audience, err := domain.NewAudience("team-b", []domain.TeamName{"team-a", "ops"})
	if err != nil {
		t.Fatal(err)
	}
	return application.CrossingEvent{Tenant: "default", Event: event, Audience: audience, TraceID: "trace-" + id.String(), Crossings: 1}
}

// heard is what a listener was handed, in order.
type heard struct {
	mu     sync.Mutex
	events []application.CrossingEvent
	fail   int
	signal chan struct{}
}

func newHeard(fail int) *heard { return &heard{fail: fail, signal: make(chan struct{}, 64)} }

func (h *heard) handle(_ context.Context, event application.CrossingEvent) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, event)
	h.signal <- struct{}{}
	if h.fail > 0 {
		h.fail--
		return errors.New("bustest: the handler fails this time")
	}
	return nil
}

// wait waits until the listener was handed n events, and answers them.
func (h *heard) wait(t *testing.T, n int) []application.CrossingEvent {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		h.mu.Lock()
		if len(h.events) >= n {
			out := append([]application.CrossingEvent(nil), h.events...)
			h.mu.Unlock()
			return out
		}
		h.mu.Unlock()
		select {
		case <-h.signal:
		case <-deadline:
			t.Fatalf("the listener was handed %d events, want %d", len(h.events), n)
		}
	}
}

func (h *heard) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.events)
}

// same reports whether the event came off the bus as it went on.
func same(a, b application.CrossingEvent) bool {
	return a.Tenant == b.Tenant && a.Event == b.Event && a.TraceID == b.TraceID && a.Crossings == b.Crossings &&
		slices.Equal(a.Audience.Strings(), b.Audience.Strings())
}

// RunDomain checks a domain-event bus adapter against the contract: an
// event reaches the listener whole, in the order published; one whose
// handler failed is not lost (given again, or its failure answered to the
// publisher); a fact published twice reaches the listener at least once
// and at most twice, so the receiver's key keeps it one task.
func RunDomain(t *testing.T, factory DomainFactory) {
	t.Run("an event reaches the listener whole and in order", func(t *testing.T) {
		bus, feed := factory(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		h := newHeard(0)
		if err := feed.Listen(ctx, h.handle); err != nil {
			t.Fatal(err)
		}
		facts := []application.CrossingEvent{Fact(t, "h-1"), Fact(t, "h-2"), Fact(t, "h-3")}
		for _, fact := range facts {
			if err := bus.Publish(ctx, fact); err != nil {
				t.Fatal(err)
			}
		}
		got := h.wait(t, 3)
		for i, fact := range facts {
			if !same(got[i], fact) {
				t.Errorf("event %d = %+v, want %+v", i, got[i], fact)
			}
		}
	})
	t.Run("an event whose handler failed is not lost", func(t *testing.T) {
		bus, feed := factory(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		h := newHeard(1)
		if err := feed.Listen(ctx, h.handle); err != nil {
			t.Fatal(err)
		}
		fact := Fact(t, "h-4")
		if err := bus.Publish(ctx, fact); err != nil {
			// The bus answered the failure: the publisher gives it again.
			if err := bus.Publish(ctx, fact); err != nil {
				t.Fatal(err)
			}
		}
		got := h.wait(t, 2)
		if !same(got[0], fact) || !same(got[1], fact) {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("a fact published twice reaches the listener once or twice", func(t *testing.T) {
		bus, feed := factory(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		h := newHeard(0)
		if err := feed.Listen(ctx, h.handle); err != nil {
			t.Fatal(err)
		}
		fact := Fact(t, "h-5")
		for range 2 {
			if err := bus.Publish(ctx, fact); err != nil {
				t.Fatal(err)
			}
		}
		if err := bus.Publish(ctx, Fact(t, "h-6")); err != nil {
			t.Fatal(err)
		}
		got := h.wait(t, 2)
		last := got[len(got)-1]
		for last.Event.ID() != "h-6" {
			got = h.wait(t, len(got)+1)
			last = got[len(got)-1]
		}
		if n := h.count(); n < 2 || n > 3 {
			t.Errorf("handed %d events", n)
		}
		for _, event := range got[:len(got)-1] {
			if event.Event.Identity() != fact.Event.Identity() {
				t.Errorf("got %+v", event)
			}
		}
	})
}
