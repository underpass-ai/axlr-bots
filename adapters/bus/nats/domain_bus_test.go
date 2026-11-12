package nats

import (
	"context"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/underpass-ai/axlr-bots/adapters/bus/bustest"
	"github.com/underpass-ai/axlr-bots/adapters/bus/domainevent"
	"github.com/underpass-ai/axlr-bots/application"
)

// domainBusOn starts a domain bus on the broker with its own outbox.
func domainBusOn(t *testing.T, url string, logs *syncLog) *DomainBus {
	t.Helper()
	logger := log.New(os.Stderr, "", 0)
	if logs != nil {
		logger = log.New(logs, "", 0)
	}
	bus, err := NewDomainBus(Config{URL: url, Outbox: filepath.Join(t.TempDir(), "outbox-domain"), Name: "test", Log: logger})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bus.Close)
	return bus
}

func TestNATSMeetsTheDomainContract(t *testing.T) {
	bustest.RunDomain(t, func(t *testing.T) (application.DomainEventBus, application.DomainEventFeed) {
		bus := domainBusOn(t, broker(t).ClientURL(), nil)
		return bus, bus
	})
}

// collect is a handler that keeps what it is handed.
type collect struct {
	mu     sync.Mutex
	events []application.CrossingEvent
}

func (c *collect) handle(_ context.Context, event application.CrossingEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
	return nil
}

func (c *collect) ids() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var ids []string
	for _, e := range c.events {
		ids = append(ids, e.Event.ID().String())
	}
	return ids
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); !ok(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("waited for %s", what)
		}
	}
}

func domainMessages(t *testing.T, js jetstream.JetStream) uint64 {
	t.Helper()
	stream, err := js.Stream(context.Background(), domainevent.Stream)
	if err != nil {
		t.Fatal(err)
	}
	info, err := stream.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return info.State.Msgs
}

// The same fact published twice is one message: the broker drops the
// repeat by its identity. What is no domain event of the spec is acked
// and logged, and the next one is read.
func TestARepeatIsDroppedAndPoisonIsTakenOff(t *testing.T) {
	url := broker(t).ClientURL()
	var logs syncLog
	bus := domainBusOn(t, url, &logs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for range 2 {
		if err := bus.Publish(ctx, bustest.Fact(t, "h-1")); err != nil {
			t.Fatal(err)
		}
	}
	js := operatorJS(t, url)
	waitFor(t, "the outbox to drain", func() bool { return len(bus.box.sweepFolder(domainOutboxFolder)) == 0 })
	if got := domainMessages(t, js); got != 1 {
		t.Errorf("the stream holds %d events, want 1", got)
	}
	if _, err := js.Publish(ctx, "axlr.domain.default.team-b.order.new", []byte(`{"specVersion": "1", "name": "stop.all"}`)); err != nil {
		t.Fatal(err)
	}
	if err := bus.Publish(ctx, bustest.Fact(t, "h-2")); err != nil {
		t.Fatal(err)
	}
	var c collect
	if err := bus.Listen(ctx, c.handle); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "two events", func() bool { return len(c.ids()) == 2 })
	if ids := c.ids(); ids[0] != "h-1" || ids[1] != "h-2" {
		t.Errorf("ids = %v", ids)
	}
	if !strings.Contains(logs.String(), "refused and dropped") {
		t.Errorf("logs = %q", logs.String())
	}
	if err := bus.Listen(ctx, nil); err == nil {
		t.Error("a listener without a handler")
	}
}

// A domain event published while the broker is away waits in the outbox,
// whatever its age, and a listener started then reads it once the broker
// is back.
func TestADomainEventWaitsForTheBroker(t *testing.T) {
	s := broker(t)
	port := s.Addr().(*net.TCPAddr).Port
	url := s.ClientURL()
	s.Shutdown()
	s.WaitForShutdown()
	var logs syncLog
	bus := domainBusOn(t, url, &logs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := bus.Publish(ctx, bustest.Fact(t, "h-away")); err != nil {
		t.Fatal(err)
	}
	var c collect
	if err := bus.Listen(ctx, c.handle); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	left := bus.box.sweepFolder(domainOutboxFolder)
	if len(left) != 1 || left[0].msgID != bustest.Fact(t, "h-away").Event.Identity() || !strings.HasPrefix(left[0].subject, "axlr.domain.default.team-b.") {
		t.Fatalf("outbox = %+v", left)
	}
	bus.Close()
	// A bus started again over the outbox publishes it once a broker
	// answers at the same address; the listener of before reconnects.
	again, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: port, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go again.Start()
	if !again.ReadyForConnections(10 * time.Second) {
		t.Fatal("the broker did not start")
	}
	defer func() { again.Shutdown(); again.WaitForShutdown() }()
	restarted, err := NewDomainBus(bus.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	waitFor(t, "the event published while the broker was away", func() bool { return len(c.ids()) == 1 })
	if !strings.Contains(logs.String(), "(retrying)") {
		t.Errorf("logs = %q", logs.String())
	}
}

// The listener's consumer is lost with the broker and found again.
func TestTheListenerComesBackWithTheBroker(t *testing.T) {
	dir := t.TempDir()
	s := brokerWith(t, func(o *server.Options) { o.StoreDir = dir })
	port := s.Addr().(*net.TCPAddr).Port
	bus := domainBusOn(t, s.ClientURL(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var c collect
	if err := bus.Listen(ctx, c.handle); err != nil {
		t.Fatal(err)
	}
	if err := bus.Publish(ctx, bustest.Fact(t, "h-1")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the first event", func() bool { return len(c.ids()) == 1 })
	s.Shutdown()
	s.WaitForShutdown()
	again, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: port, JetStream: true, StoreDir: dir, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go again.Start()
	if !again.ReadyForConnections(10 * time.Second) {
		t.Fatal("the broker did not start")
	}
	defer func() { again.Shutdown(); again.WaitForShutdown() }()
	if err := bus.Publish(ctx, bustest.Fact(t, "h-2")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the event after the broker came back", func() bool { return len(c.ids()) == 2 })
}

func TestTheDomainBusNeedsItsBrokerAndOutbox(t *testing.T) {
	if _, err := NewDomainBus(Config{}); err == nil {
		t.Error("a domain bus without a URL")
	}
	if _, err := NewDomainBus(Config{URL: "nats://127.0.0.1:1"}); err == nil {
		t.Error("a domain bus without an outbox")
	}
}

type fakeMeta struct {
	jetstream.Msg
	delivered uint64
}

func (m fakeMeta) Metadata() (*jetstream.MsgMetadata, error) {
	return &jetstream.MsgMetadata{NumDelivered: m.delivered}, nil
}

func TestTheRedeliveryDelayGrows(t *testing.T) {
	for delivered, want := range map[uint64]time.Duration{0: 250 * time.Millisecond, 1: 250 * time.Millisecond, 2: 500 * time.Millisecond, 4: 2 * time.Second, 40: time.Minute} {
		if got := redeliveryDelay(fakeMeta{delivered: delivered}); got != want {
			t.Errorf("%d deliveries: %s, want %s", delivered, got, want)
		}
	}
}
