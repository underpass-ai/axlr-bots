package nats

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	natsio "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/underpass-ai/axlr-bots/adapters/bus/bustest"
	"github.com/underpass-ai/axlr-bots/adapters/bus/topicwire"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// streamUpdates counts the stream updates asked of the broker, whoever
// asks: it listens on the JetStream API subject they go to.
type streamUpdates struct {
	conn *natsio.Conn
	msgs chan *natsio.Msg
}

func spyStreamUpdates(t *testing.T, url string) streamUpdates {
	t.Helper()
	conn, err := natsio.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	spy := streamUpdates{conn: conn, msgs: make(chan *natsio.Msg, 256)}
	if _, err := conn.ChanSubscribe("$JS.API.STREAM.UPDATE.>", spy.msgs); err != nil {
		t.Fatal(err)
	}
	if err := conn.Flush(); err != nil {
		t.Fatal(err)
	}
	return spy
}

// count is how many updates were asked so far: the flush makes the
// broker hand over every request routed before it.
func (s streamUpdates) count(t *testing.T) int {
	t.Helper()
	if err := s.conn.Flush(); err != nil {
		t.Fatal(err)
	}
	return len(s.msgs)
}

// streamConfig is the configuration the broker keeps for the stream.
func streamConfig(t *testing.T, js jetstream.JetStream, name string) jetstream.StreamConfig {
	t.Helper()
	stream, err := js.Stream(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return stream.CachedInfo().Config
}

// topicsDefined starts the topics and their consumer, and returns once
// both connections have defined the streams: the n-th copy is in
// AXLR_TOPIC and the n-th publication reached the service.
func topicsDefined(t *testing.T, cfg Config, js jetstream.JetStream, n int) {
	t.Helper()
	cfg.Outbox = filepath.Join(t.TempDir(), "outbox-topics")
	topics := topicsOn(t, cfg)
	svc := &service{groups: teams()}
	ctx, cancel := context.WithCancel(context.Background())
	if err := topics.Listen(ctx, svc); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); topics.Wait(); topics.Close() }()
	id := fmt.Sprintf("evt-%d", n)
	if err := topics.Tap(&recording{}).Publish(ctx, botEvent(domain.EventID(id), "", "team-a")); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the copy", func() bool {
		stream, err := js.Stream(ctx, topicwire.TopicStream)
		return err == nil && stream.CachedInfo().State.Msgs == uint64(n)
	})
	msg, err := topicwire.EncodePublication("default", "team-a", fmt.Sprintf("k-%d", n), topicwire.PublicationDTO{Name: "order.placed", Message: "m"})
	if err != nil {
		t.Fatal(err)
	}
	publishOn(t, js, msg.Subject, msg.MsgID, string(msg.Payload))
	waitUntil(t, "the publication", func() bool { got, _ := svc.counts(); return got == 1 })
}

// pushDefined starts a publisher and returns once a push is in its
// stream, so its connection has defined the stream.
func pushDefined(t *testing.T, url string) {
	t.Helper()
	publisher := publisherOn(t, url)
	if err := publisher.Push(context.Background(), bustest.NodeA, application.NodePush{Execution: "exec"}); err != nil {
		t.Fatal(err)
	}
	drained(t, publisher, 10*time.Second)
	publisher.Close()
}

// Connecting again with the streams as they are asks for no update, on
// any of the three streams: an update rewrites the stream's store and
// races a consumer created at the same moment in nats-server. A stream
// an operator changed is put back.
func TestConnectingAgainUpdatesNoStreamThatMatches(t *testing.T) {
	url := broker(t).ClientURL()
	spy := spyStreamUpdates(t, url)
	js := operatorJS(t, url)
	cfg := Config{URL: url, Name: "test"}
	for n := 1; n <= 2; n++ {
		topicsDefined(t, cfg, js, n)
		pushDefined(t, url)
	}
	if got := spy.count(t); got != 0 {
		t.Fatalf("%d stream updates asked for streams that match", got)
	}

	changed := pushStream(cfg)
	changed.MaxAge = 2 * Retention
	if _, err := js.UpdateStream(context.Background(), changed); err != nil {
		t.Fatal(err)
	}
	if got := spy.count(t); got != 1 {
		t.Fatalf("the spy saw %d updates, want the operator's", got)
	}
	pushDefined(t, url)
	if got := spy.count(t); got != 2 {
		t.Errorf("%d stream updates, want the operator's and the one putting it back", got)
	}
	if got := streamConfig(t, js, DefaultStream).MaxAge; got != Retention {
		t.Errorf("the push stream keeps %s, want %s", got, Retention)
	}
}

// Topics whose window changed update both their streams, once each.
func TestChangedTopicStreamsAreUpdated(t *testing.T) {
	url := broker(t).ClientURL()
	spy := spyStreamUpdates(t, url)
	js := operatorJS(t, url)
	topicsDefined(t, Config{URL: url, Name: "test"}, js, 1)
	if got := spy.count(t); got != 0 {
		t.Fatalf("%d stream updates on streams just created", got)
	}
	topicsDefined(t, Config{URL: url, Name: "test", TopicDuplicates: time.Minute}, js, 2)
	if got := spy.count(t); got < 2 {
		t.Errorf("%d stream updates, want both topic streams'", got)
	}
	for _, name := range []string{topicwire.TopicStream, topicwire.PublishStream} {
		if got := streamConfig(t, js, name).Duplicates; got != time.Minute {
			t.Errorf("%s drops repeats within %s, want a minute", name, got)
		}
	}
}

// missing is a JetStream client that does not find the stream the first
// misses times it reads it, as one that read it before another
// connection created it.
type missing struct {
	jetstream.JetStream
	misses int
}

func (m *missing) Stream(ctx context.Context, name string) (jetstream.Stream, error) {
	if m.misses > 0 {
		m.misses--
		return nil, jetstream.ErrStreamNotFound
	}
	return m.JetStream.Stream(ctx, name)
}

// A stream another connection created between the read and the create
// is read again and put right; one that never shows up gives up.
func TestAStreamCreatedMeanwhileIsReadAgain(t *testing.T) {
	url := broker(t).ClientURL()
	js := operatorJS(t, url)
	want := pushStream(Config{})
	other := want
	other.MaxAge = 2 * Retention
	if _, err := js.CreateStream(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	if err := defineStream(context.Background(), &missing{JetStream: js, misses: 1}, want); err != nil {
		t.Fatal(err)
	}
	if got := streamConfig(t, js, want.Name).MaxAge; got != Retention {
		t.Errorf("the stream keeps %s, want %s", got, Retention)
	}

	other.Name, want.Name = "OTHER", "OTHER"
	other.Subjects, want.Subjects = []string{"other.>"}, []string{"other.>"}
	if _, err := js.CreateStream(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	err := defineStream(context.Background(), &missing{JetStream: js, misses: defineAttempts}, want)
	if !errors.Is(err, jetstream.ErrStreamNameAlreadyInUse) {
		t.Errorf("err = %v, want the name in use", err)
	}
}

// What axlr-bots defines is compared; an unset byte limit is the
// broker's -1.
func TestStreamDiffers(t *testing.T) {
	want := topicStream(Config{})
	have := want
	have.Description, have.Replicas = "the broker's", 1
	if streamDiffers(have, want) {
		t.Error("a stream that differs in no field axlr-bots defines differs")
	}
	unset := pushStream(Config{})
	kept := unset
	kept.MaxBytes = -1
	if streamDiffers(kept, unset) {
		t.Error("the broker's -1 differs from an unset byte limit")
	}
	for name, change := range map[string]func(*jetstream.StreamConfig){
		"subjects":   func(c *jetstream.StreamConfig) { c.Subjects = []string{"x.>"} },
		"retention":  func(c *jetstream.StreamConfig) { c.Retention = jetstream.WorkQueuePolicy },
		"storage":    func(c *jetstream.StreamConfig) { c.Storage = jetstream.MemoryStorage },
		"max age":    func(c *jetstream.StreamConfig) { c.MaxAge = time.Minute },
		"max bytes":  func(c *jetstream.StreamConfig) { c.MaxBytes = 1 },
		"duplicates": func(c *jetstream.StreamConfig) { c.Duplicates = time.Second },
		"discard":    func(c *jetstream.StreamConfig) { c.Discard = jetstream.DiscardNew },
	} {
		have := want
		change(&have)
		if !streamDiffers(have, want) {
			t.Errorf("a stream with other %s does not differ", name)
		}
	}
}
