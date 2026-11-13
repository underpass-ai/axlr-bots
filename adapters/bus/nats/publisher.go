package nats

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	natsio "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// queueSize bounds the pushes waiting in memory; past it a push waits
// in the outbox alone until the next sweep.
const queueSize = 4096

// sweepEvery is how often the publisher looks in the outbox for pushes
// the queue did not take.
const sweepEvery = 30 * time.Second

// maxRefusals is how many times the broker may refuse one push for good
// (refused) before the publisher drops it: a push it can never take
// must not hold back the ones behind it.
const maxRefusals = 5

// Publisher is the control plane's side: application.NodePusher over
// the broker. Push writes the outbox and returns; one goroutine
// publishes in order, retrying while the broker is away, and removes
// each push from the outbox once the broker acknowledged it.
type Publisher struct {
	cfg    Config
	box    outbox
	queue  chan entry
	seq    atomic.Uint64
	stop   chan struct{}
	done   chan struct{}
	once   sync.Once
	inMu   sync.Mutex
	queued map[string]bool
}

var _ application.NodePusher = (*Publisher)(nil)

// NewPublisher opens the outbox, requeues what was left and starts
// publishing. It does not wait for the broker.
func NewPublisher(cfg Config) (*Publisher, error) {
	if cfg.URL == "" {
		return nil, errors.New("nats: the broker's URL is required")
	}
	if cfg.Outbox == "" {
		return nil, errors.New("nats: the publisher needs an outbox directory")
	}
	p := &Publisher{cfg: cfg, box: outbox{dir: cfg.Outbox}, queue: make(chan entry, queueSize), stop: make(chan struct{}), done: make(chan struct{}), queued: map[string]bool{}}
	p.requeue()
	go p.run()
	return p, nil
}

// Push keeps the push in the outbox and queues it; it never waits for
// the broker.
func (p *Publisher) Push(_ context.Context, node domain.NodeID, push application.NodePush) error {
	e, err := p.box.add(node, p.seq.Add(1), encode(push))
	if err != nil {
		return err
	}
	p.enqueue(e)
	return nil
}

// Close stops publishing; what the broker did not take stays in the
// outbox for the next start.
func (p *Publisher) Close() {
	p.once.Do(func() { close(p.stop) })
	<-p.done
}

func (p *Publisher) enqueue(e entry) {
	p.inMu.Lock()
	defer p.inMu.Unlock()
	if p.queued[e.id] {
		return
	}
	select {
	case p.queue <- e:
		p.queued[e.id] = true
	default:
		// The queue is full: the file waits for a sweep.
	}
}

func (p *Publisher) dequeued(e entry) {
	p.inMu.Lock()
	defer p.inMu.Unlock()
	delete(p.queued, e.id)
}

// requeue queues what the outbox holds, dropping the pushes older than
// the stream keeps them: a node that comes back after that resyncs.
func (p *Publisher) requeue() {
	stale := 0
	for _, e := range p.box.sweep() {
		if e.olderThan(Retention, time.Now()) {
			p.box.remove(e)
			stale++
			continue
		}
		p.enqueue(e)
	}
	if stale > 0 {
		p.cfg.log().Printf("bus: nats: dropped %d pushes older than %s from the outbox", stale, Retention)
	}
}

// run publishes the queue in order until Close.
func (p *Publisher) run() {
	defer close(p.done)
	logger := p.cfg.log()
	var js jetstream.JetStream
	var conn *natsio.Conn
	defer func() {
		if conn != nil {
			_ = conn.Drain()
		}
	}()
	sweep := time.NewTicker(sweepEvery)
	defer sweep.Stop()
	// Connect at once, so the stream exists before the first push: a
	// node finds the stream, it does not define it.
	connected := func() bool {
		for js == nil {
			var err error
			if conn, js, err = connect(p.cfg, true); err != nil {
				logger.Printf("bus: nats: %v (retrying)", err)
				if !p.pause(2 * time.Second) {
					return false
				}
				continue
			}
			logger.Printf("bus: nats: publishing to %s on %s", p.cfg.stream(), conn.ConnectedUrl())
		}
		return true
	}
	if !connected() {
		return
	}
	for {
		var e entry
		select {
		case <-p.stop:
			return
		case <-sweep.C:
			p.requeue()
			continue
		case e = <-p.queue:
		}
		refusals := 0
		for {
			if e.olderThan(Retention, time.Now()) {
				// The broker would not keep it now anyway.
				application.LogWith(logger, application.LogSubject{Node: e.node}).Printf("bus: nats: push %s to %s dropped: older than %s", e.id, e.node, Retention)
				p.box.remove(e)
				p.dequeued(e)
				break
			}
			if !connected() {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := js.Publish(ctx, e.target(), e.payload, jetstream.WithMsgID(e.publishID()))
			cancel()
			if err == nil {
				p.box.remove(e)
				p.dequeued(e)
				break
			}
			if refused(err) {
				refusals++
				if refusals >= maxRefusals {
					application.LogWith(logger, application.LogSubject{Node: e.node}).Printf("bus: nats: push %s to %s dropped: refused %d times: %v", e.id, e.node, refusals, err)
					p.box.remove(e)
					p.dequeued(e)
					break
				}
			}
			application.LogWith(logger, application.LogSubject{Node: e.node}).Printf("bus: nats: push to %s not taken: %v (retrying)", e.node, err)
			if conn.IsClosed() || streamSuspect(err) {
				// A stream an operator deleted, or one that refuses the
				// push, is defined again on the next connection.
				conn.Close()
				js, conn = nil, nil
			}
			if !p.pause(time.Second) {
				return
			}
		}
	}
}

// refused reports whether the broker refused the push for good, as
// opposed to being away: JetStream refused it (rejected) or it is
// larger than the server takes.
func refused(err error) bool {
	return errors.Is(err, natsio.ErrMaxPayload) || rejected(err)
}

// streamSuspect reports whether the stream itself may be what is
// wrong: nothing answered for it (an operator deleted it) or JetStream
// rejected the push.
func streamSuspect(err error) bool {
	return errors.Is(err, jetstream.ErrNoStreamResponse) || rejected(err)
}

// rejected reports a JetStream API error of the 4xx kind other than a
// timeout: the request was wrong, and asking again will not mend it.
func rejected(err error) bool {
	var jsErr jetstream.JetStreamError
	if !errors.As(err, &jsErr) {
		return false
	}
	api := jsErr.APIError()
	return api != nil && api.Code >= 400 && api.Code < 500 && api.Code != 408
}

func (p *Publisher) pause(d time.Duration) bool {
	select {
	case <-p.stop:
		return false
	case <-time.After(d):
		return true
	}
}

// options are the connection's: its name, reconnecting for ever, and
// the credentials or the client certificate the config names.
func options(cfg Config) ([]natsio.Option, error) {
	options := []natsio.Option{natsio.Name(cfg.Name), natsio.Timeout(5 * time.Second), natsio.MaxReconnects(-1), natsio.ReconnectWait(time.Second)}
	if cfg.Credentials != "" {
		options = append(options, natsio.UserCredentials(cfg.Credentials))
	}
	if (cfg.CertFile == "") != (cfg.KeyFile == "") {
		return nil, errors.New("nats: a client certificate needs its key, and a key its certificate")
	}
	if cfg.CertFile != "" {
		options = append(options, natsio.ClientCert(cfg.CertFile, cfg.KeyFile))
	}
	if cfg.CAFile != "" {
		options = append(options, natsio.RootCAs(cfg.CAFile))
	}
	return options, nil
}

// connect opens the connection. With define it makes sure the push
// stream exists (the control plane's); without, it finds the one the
// control plane defined: a node has no right to define it, and asking
// would cost it a timeout on every start.
func connect(cfg Config, define bool) (*natsio.Conn, jetstream.JetStream, error) {
	return connectStream(cfg, pushStream(cfg), define)
}

// pushStream is the stream that keeps the pushes to the nodes.
func pushStream(cfg Config) jetstream.StreamConfig {
	return jetstream.StreamConfig{
		Name: cfg.stream(), Subjects: []string{SubjectPrefix + ".>"}, Retention: jetstream.LimitsPolicy, Storage: jetstream.FileStorage,
		MaxAge: Retention, Duplicates: Duplicates, Discard: jetstream.DiscardOld,
	}
}

// connectStream opens the connection and defines the stream (define) or
// finds it.
func connectStream(cfg Config, stream jetstream.StreamConfig, define bool) (*natsio.Conn, jetstream.JetStream, error) {
	opts, err := options(cfg)
	if err != nil {
		return nil, nil, err
	}
	conn, err := natsio.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, nil, err
	}
	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !define {
		// The control plane defines the stream when it connects; a node
		// that comes first waits for it a little.
		for {
			_, err := js.Stream(ctx, stream.Name)
			if err == nil {
				return conn, js, nil
			}
			if !errors.Is(err, jetstream.ErrStreamNotFound) || ctx.Err() != nil {
				conn.Close()
				return nil, nil, fmt.Errorf("stream %s: %w", stream.Name, err)
			}
			select {
			case <-ctx.Done():
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	if _, err := js.CreateOrUpdateStream(ctx, stream); err != nil {
		conn.Close()
		return nil, nil, err
	}
	return conn, js, nil
}
