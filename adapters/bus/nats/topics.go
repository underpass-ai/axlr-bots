package nats

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	natsio "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/underpass-ai/axlr-bots/adapters/bus/topicwire"
	"github.com/underpass-ai/axlr-bots/domain"
)

// A team's topics on the broker (docs/architecture/topics-on-nats.md):
// how long the streams keep a message, how much the topics may hold,
// and the outbox folder the copies wait in.
const (
	// TopicRetention is how long the broker keeps a team's event and an
	// application's publication.
	TopicRetention = 24 * time.Hour
	// TopicMaxBytes bounds what AXLR_TOPIC holds; past it the oldest go.
	TopicMaxBytes = 1 << 30
	// topicOutboxFolder is the outbox folder the copies wait in.
	topicOutboxFolder = "topic"
)

// Topics is the control plane's side of a team's topics over the
// broker. Its Tap decorates the event publisher: once an event is
// recorded, each copy for a team it was told to is written to the
// outbox, and one goroutine publishes the outbox in order, with the
// event's id and team as Nats-Msg-Id (the broker drops a repeat within
// its window), retrying while the broker is away. Listen reads the
// publications applications make on a team and publishes them as the
// operator. Neither ever holds up a use case.
type Topics struct {
	cfg     Config
	box     outbox
	seq     atomic.Uint64
	mu      sync.Mutex
	pending []entry
	wake    chan struct{}
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
	// listening is the consumer's goroutine, which Wait joins.
	listening sync.WaitGroup
}

// topicStream keeps a team's topics: every subject under axlr.topic,
// file storage, a day, a gigabyte, a republish dropped within the
// duplicates window.
func topicStream(cfg Config) jetstream.StreamConfig {
	return jetstream.StreamConfig{
		Name: topicwire.TopicStream, Subjects: []string{topicwire.TopicPrefix + ".>"}, Retention: jetstream.LimitsPolicy, Storage: jetstream.FileStorage,
		MaxAge: TopicRetention, MaxBytes: TopicMaxBytes, Duplicates: cfg.duplicatesWindow(), Discard: jetstream.DiscardOld,
	}
}

// publishStream keeps the publications applications make on a team.
func publishStream(cfg Config) jetstream.StreamConfig {
	return jetstream.StreamConfig{
		Name: topicwire.PublishStream, Subjects: []string{topicwire.PublishPrefix + ".>"}, Retention: jetstream.LimitsPolicy, Storage: jetstream.FileStorage,
		MaxAge: TopicRetention, Duplicates: cfg.duplicatesWindow(), Discard: jetstream.DiscardOld,
	}
}

// NewTopics opens the outbox (Config.Outbox), queues what was left and
// starts publishing: it connects at once and defines both streams, so
// an application can make its consumer before the first event. It does
// not wait for the broker.
func NewTopics(cfg Config) (*Topics, error) {
	if cfg.URL == "" {
		return nil, errors.New("nats: the broker's URL is required")
	}
	if cfg.Outbox == "" {
		return nil, errors.New("nats: the topics need an outbox directory")
	}
	t := &Topics{cfg: cfg, box: outbox{dir: cfg.Outbox}, wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	t.pending = t.box.sweepFolder(topicOutboxFolder)
	go t.run()
	return t, nil
}

// publish writes the event's copies to the outbox and queues them; an
// event that is no team's topic writes none.
func (t *Topics) publish(event domain.Event) error {
	messages, err := topicwire.EncodeTopics(event)
	if err != nil || len(messages) == 0 {
		return err
	}
	for _, message := range messages {
		e, err := t.box.addMessage(topicOutboxFolder, message.Subject, message.MsgID, t.seq.Add(1), message.Payload)
		if err != nil {
			return err
		}
		t.mu.Lock()
		t.pending = append(t.pending, e)
		t.mu.Unlock()
	}
	select {
	case t.wake <- struct{}{}:
	default:
	}
	return nil
}

// Close stops publishing; what the broker did not take stays in the
// outbox for the next start. Listening ends with its own context.
func (t *Topics) Close() {
	t.once.Do(func() { close(t.stop) })
	<-t.done
}

// next is the oldest copy not yet published; false when there is none.
func (t *Topics) next() (entry, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.pending) == 0 {
		return entry{}, false
	}
	return t.pending[0], true
}

// taken forgets the oldest copy: the broker took it, or it is dropped.
func (t *Topics) taken(e entry) {
	t.box.remove(e)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending = t.pending[1:]
}

// run publishes the queue in order until Close.
func (t *Topics) run() {
	defer close(t.done)
	logger := t.cfg.log()
	var conn *natsio.Conn
	var js jetstream.JetStream
	defer func() {
		if conn != nil {
			_ = conn.Drain()
		}
	}()
	delay, refusals := time.Second, 0
	for {
		if js == nil {
			var err error
			if conn, js, err = connectDefining(t.cfg, topicStream(t.cfg), publishStream(t.cfg)); err != nil {
				logger.Printf("bus: nats: topics: %v (retrying)", err)
				if !t.pause(2 * time.Second) {
					return
				}
				continue
			}
			logger.Printf("bus: nats: publishing a team's topics to %s on %s", topicwire.TopicStream, conn.ConnectedUrl())
		}
		e, ok := t.next()
		if !ok {
			select {
			case <-t.stop:
				return
			case <-t.wake:
				continue
			}
		}
		if e.olderThan(TopicRetention, time.Now()) {
			logger.Printf("bus: nats: topic %s dropped: older than %s", e.publishID(), TopicRetention)
			t.taken(e)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := js.Publish(ctx, e.target(), e.payload, jetstream.WithMsgID(e.publishID()))
		cancel()
		if err == nil {
			t.taken(e)
			delay, refusals = time.Second, 0
			continue
		}
		wait := delay
		if refused(err) {
			if refusals++; refusals >= maxRefusals {
				// One the broker never takes must not hold back the rest.
				logger.Printf("bus: nats: topic %s dropped: refused %d times: %v", e.publishID(), refusals, err)
				t.taken(e)
				delay, refusals = time.Second, 0
				continue
			}
			// A refusal is the broker answering: try again soon, not later
			// and later as for a broker that is away.
			wait = time.Second
		} else {
			delay = min(2*delay, 30*time.Second)
		}
		logger.Printf("bus: nats: topic %s not taken: %v (retrying)", e.publishID(), err)
		if conn.IsClosed() || streamSuspect(err) {
			conn.Close()
			conn, js = nil, nil
		}
		if !t.pause(wait) {
			return
		}
	}
}

func (t *Topics) pause(d time.Duration) bool {
	select {
	case <-t.stop:
		return false
	case <-time.After(d):
		return true
	}
}

// connectDefining opens the connection and creates or updates the
// streams.
func connectDefining(cfg Config, streams ...jetstream.StreamConfig) (*natsio.Conn, jetstream.JetStream, error) {
	conn, js, err := connectStream(cfg, streams[0], true)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, stream := range streams[1:] {
		if _, err := js.CreateOrUpdateStream(ctx, stream); err != nil {
			conn.Close()
			return nil, nil, err
		}
	}
	return conn, js, nil
}
