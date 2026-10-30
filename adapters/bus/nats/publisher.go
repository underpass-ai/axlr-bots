package nats

import (
	"context"
	"errors"
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

func (p *Publisher) requeue() {
	for _, e := range p.box.sweep() {
		p.enqueue(e)
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
		for {
			if js == nil {
				var err error
				if conn, js, err = connect(p.cfg); err != nil {
					logger.Printf("bus: nats: %v (retrying)", err)
					if !p.pause(2 * time.Second) {
						return
					}
					continue
				}
				logger.Printf("bus: nats: publishing to %s on %s", p.cfg.stream(), conn.ConnectedUrl())
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := js.Publish(ctx, subject(e.node), e.payload, jetstream.WithMsgID(e.id))
			cancel()
			if err == nil {
				p.box.remove(e)
				p.dequeued(e)
				break
			}
			logger.Printf("bus: nats: push to %s not taken: %v (retrying)", e.node, err)
			if conn.IsClosed() {
				js, conn = nil, nil
			}
			if !p.pause(time.Second) {
				return
			}
		}
	}
}

func (p *Publisher) pause(d time.Duration) bool {
	select {
	case <-p.stop:
		return false
	case <-time.After(d):
		return true
	}
}

// connect opens the connection and makes sure the stream exists.
func connect(cfg Config) (*natsio.Conn, jetstream.JetStream, error) {
	options := []natsio.Option{natsio.Name(cfg.Name), natsio.Timeout(5 * time.Second), natsio.MaxReconnects(-1), natsio.ReconnectWait(time.Second)}
	if cfg.Credentials != "" {
		options = append(options, natsio.UserCredentials(cfg.Credentials))
	}
	conn, err := natsio.Connect(cfg.URL, options...)
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
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name: cfg.stream(), Subjects: []string{SubjectPrefix + ".>"}, Retention: jetstream.LimitsPolicy, Storage: jetstream.FileStorage,
		MaxAge: Retention, Duplicates: Duplicates, Discard: jetstream.DiscardOld,
	}); err != nil {
		// A connection without the right to define the stream (a node's)
		// uses the one the control plane defined.
		if _, lookup := js.Stream(ctx, cfg.stream()); lookup != nil {
			conn.Close()
			return nil, nil, err
		}
	}
	return conn, js, nil
}
