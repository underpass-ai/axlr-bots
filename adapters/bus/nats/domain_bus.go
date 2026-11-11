package nats

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	natsio "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/underpass-ai/axlr-bots/adapters/bus/domainevent"
	"github.com/underpass-ai/axlr-bots/application"
)

// Domain events between teams (docs/architecture/team-topics.md): the
// stream that keeps them, how long, and the control plane's consumer.
const (
	// DomainRetention is how long the broker keeps a domain event: a fact
	// stays readable a week after it happened, acked or not, for the
	// audit and for a control plane that was away that long.
	DomainRetention = 7 * 24 * time.Hour
	// DomainConsumer is the control plane's durable consumer of every
	// domain event.
	DomainConsumer = "control-plane"
	// domainOutboxFolder is the folder of the outbox the domain events
	// wait in, beside the nodes' pushes.
	domainOutboxFolder = "domain"
	// domainAckWait is how long the broker waits for the handler before
	// it gives the event again.
	domainAckWait = time.Minute
	// maxRedeliveryDelay bounds the wait before an event whose handler
	// failed is given again.
	maxRedeliveryDelay = time.Minute
)

// DomainBus is application.DomainEventBus and application.DomainEventFeed
// over the broker. Publish writes the event to the outbox and returns;
// one goroutine publishes the outbox in order, with the event's identity
// as Nats-Msg-Id (the broker drops a repeat within its window), retrying
// while the broker is away and dropping nothing: a domain event is a
// fact, not a hint. Listen reads every domain event with one durable
// consumer, acks it once the handler took it, gives it again (with a
// growing delay) when the handler fails, and acks and logs one that is
// no domain event of the spec, which retrying would never mend.
type DomainBus struct {
	cfg     Config
	box     outbox
	seq     atomic.Uint64
	mu      sync.Mutex
	pending []entry
	wake    chan struct{}
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
}

var (
	_ application.DomainEventBus  = (*DomainBus)(nil)
	_ application.DomainEventFeed = (*DomainBus)(nil)
)

// domainStream is the stream of the domain events: every subject under
// axlr.domain, file storage, a republish dropped within 2 minutes.
func domainStream() jetstream.StreamConfig {
	return jetstream.StreamConfig{
		Name: domainevent.Stream, Subjects: []string{domainevent.SubjectPrefix + ".>"}, Retention: jetstream.LimitsPolicy, Storage: jetstream.FileStorage,
		MaxAge: DomainRetention, Duplicates: Duplicates, Discard: jetstream.DiscardOld,
	}
}

// NewDomainBus opens the outbox (Config.Outbox, its domain folder),
// queues what was left and starts publishing. It does not wait for the
// broker.
func NewDomainBus(cfg Config) (*DomainBus, error) {
	if cfg.URL == "" {
		return nil, errors.New("nats: the broker's URL is required")
	}
	if cfg.Outbox == "" {
		return nil, errors.New("nats: the domain bus needs an outbox directory")
	}
	b := &DomainBus{cfg: cfg, box: outbox{dir: cfg.Outbox}, wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	b.pending = b.box.sweepFolder(domainOutboxFolder)
	go b.run()
	return b, nil
}

// Publish keeps the event in the outbox and queues it; it never waits for
// the broker.
func (b *DomainBus) Publish(_ context.Context, event application.CrossingEvent) error {
	message, err := domainevent.Encode(event)
	if err != nil {
		return err
	}
	e, err := b.box.addMessage(domainOutboxFolder, message.Subject, message.ID, b.seq.Add(1), message.Payload)
	if err != nil {
		return err
	}
	b.mu.Lock()
	b.pending = append(b.pending, e)
	b.mu.Unlock()
	select {
	case b.wake <- struct{}{}:
	default:
	}
	return nil
}

// Close stops publishing; what the broker did not take stays in the
// outbox for the next start. Listening ends with its own context.
func (b *DomainBus) Close() {
	b.once.Do(func() { close(b.stop) })
	<-b.done
}

// next is the oldest event not yet published; false when there is none.
func (b *DomainBus) next() (entry, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.pending) == 0 {
		return entry{}, false
	}
	return b.pending[0], true
}

func (b *DomainBus) published() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pending = b.pending[1:]
}

// run publishes the queue in order until Close.
func (b *DomainBus) run() {
	defer close(b.done)
	logger := b.cfg.log()
	var conn *natsio.Conn
	var js jetstream.JetStream
	defer func() {
		if conn != nil {
			_ = conn.Drain()
		}
	}()
	delay := time.Second
	for {
		e, ok := b.next()
		if !ok {
			select {
			case <-b.stop:
				return
			case <-b.wake:
				continue
			}
		}
		if js == nil {
			var err error
			if conn, js, err = connectStream(b.cfg, domainStream(), true); err != nil {
				logger.Printf("bus: nats: domain events: %v (retrying)", err)
				if !b.pause(2 * time.Second) {
					return
				}
				continue
			}
			logger.Printf("bus: nats: publishing domain events to %s on %s", domainevent.Stream, conn.ConnectedUrl())
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := js.Publish(ctx, e.target(), e.payload, jetstream.WithMsgID(e.publishID()))
		cancel()
		if err == nil {
			b.box.remove(e)
			b.published()
			delay = time.Second
			continue
		}
		logger.Printf("bus: nats: domain event %s not taken: %v (retrying)", e.publishID(), err)
		if conn.IsClosed() || streamSuspect(err) {
			conn.Close()
			conn, js = nil, nil
		}
		if !b.pause(delay) {
			return
		}
		delay = min(2*delay, 30*time.Second)
	}
}

func (b *DomainBus) pause(d time.Duration) bool {
	select {
	case <-b.stop:
		return false
	case <-time.After(d):
		return true
	}
}

// Listen reads the domain events with the control plane's durable
// consumer, from the first the stream keeps that it has not acked, and
// hands each to the handler, one at a time, until ctx ends. It returns
// once the consumer is defined, or at once with the broker away: it
// connects when the broker answers, and again whenever it is lost.
func (b *DomainBus) Listen(ctx context.Context, handle application.DomainEventHandler) error {
	if handle == nil {
		return errors.New("nats: a listener needs a handler")
	}
	conn, messages, err := b.consume(ctx)
	go func() {
		for {
			for err != nil {
				b.cfg.log().Printf("bus: nats: domain events: %v (retrying)", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
				conn, messages, err = b.consume(ctx)
			}
			b.read(ctx, conn, messages, handle)
			if ctx.Err() != nil {
				return
			}
			conn, messages, err = b.consume(ctx)
		}
	}()
	return nil
}

// consume connects, defines the stream and the consumer, and opens its
// messages.
func (b *DomainBus) consume(ctx context.Context) (*natsio.Conn, jetstream.MessagesContext, error) {
	conn, js, err := connectStream(b.cfg, domainStream(), true)
	if err != nil {
		return nil, nil, err
	}
	setup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	consumer, err := js.CreateOrUpdateConsumer(setup, domainevent.Stream, jetstream.ConsumerConfig{
		Durable: DomainConsumer, FilterSubject: domainevent.SubjectPrefix + ".>", AckPolicy: jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy, AckWait: domainAckWait, MaxAckPending: 256,
	})
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	messages, err := consumer.Messages()
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	b.cfg.log().Printf("bus: nats: the control plane reads %s.> on %s", domainevent.SubjectPrefix, conn.ConnectedUrl())
	return conn, messages, nil
}

// read hands the consumer's messages to the handler until ctx ends or
// the broker is lost.
func (b *DomainBus) read(ctx context.Context, conn *natsio.Conn, messages jetstream.MessagesContext, handle application.DomainEventHandler) {
	logger := b.cfg.log()
	defer conn.Close()
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			messages.Stop()
		case <-stopped:
		}
	}()
	for {
		msg, err := messages.Next()
		if err != nil {
			if ctx.Err() == nil {
				logger.Printf("bus: nats: domain events: %v", err)
			}
			messages.Stop()
			return
		}
		event, err := domainevent.Decode(msg.Data())
		if err != nil {
			// Poison: no domain event of the spec. Taken off, never given
			// again: retrying would never mend it.
			logger.Printf("bus: nats: domain event on %s refused and dropped: %v", msg.Subject(), err)
			b.ack(ctx, msg)
			continue
		}
		if err := handle(ctx, event); err != nil {
			delay := redeliveryDelay(msg)
			logger.Printf("bus: nats: domain event %s not delivered: %v (again in %s)", event.Event.Identity(), err, delay)
			_ = msg.NakWithDelay(delay)
			continue
		}
		b.ack(ctx, msg)
	}
}

// ack acks the message and waits for the broker to confirm it: an ack the
// broker never took (its permission missing, the broker gone) is logged,
// and the event comes again after the ack wait, a repeat the receiver's
// key makes nothing of.
func (b *DomainBus) ack(ctx context.Context, msg jetstream.Msg) {
	confirm, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := msg.DoubleAck(confirm); err != nil {
		b.cfg.log().Printf("bus: nats: domain event on %s not acked: %v", msg.Subject(), err)
	}
}

// redeliveryDelay is how long a failed event waits before it is given
// again: a quarter second, doubled for each delivery, at most a minute.
func redeliveryDelay(msg jetstream.Msg) time.Duration {
	delivered := uint64(1)
	if meta, err := msg.Metadata(); err == nil && meta.NumDelivered > 0 {
		delivered = meta.NumDelivered
	}
	delay := 250 * time.Millisecond
	for i := uint64(1); i < delivered && delay < maxRedeliveryDelay; i++ {
		delay *= 2
	}
	return min(delay, maxRedeliveryDelay)
}
