package nats

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	natsio "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/underpass-ai/axlr-bots/adapters/bus/topicwire"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

var recorded = time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)

// topicsOn starts the topics on the broker with an outbox of their own.
func topicsOn(t *testing.T, cfg Config) *Topics {
	t.Helper()
	if cfg.Outbox == "" {
		cfg.Outbox = filepath.Join(t.TempDir(), "outbox-topics")
	}
	if cfg.Log == nil {
		cfg.Log = log.New(os.Stderr, "", 0)
	}
	topics, err := NewTopics(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(topics.Close)
	return topics
}

// recording is an event publisher that keeps what it records, or fails.
type recording struct {
	mu     sync.Mutex
	events []domain.Event
	fail   error
}

func (r *recording) Publish(_ context.Context, event domain.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return r.fail
	}
	r.events = append(r.events, event)
	return nil
}

// botEvent is a bot's event told to its teams, as EmitEvent records it.
func botEvent(id domain.EventID, by string, teams ...string) domain.Event {
	payload := map[string]any{"eventId": "evt-c-" + id.String(), "name": "order.placed", "from": "bot-a", "severity": "info", "message": "order o-1 placed",
		"data": map[string]any{"order": "o-1"}, "to": []string{}, "teams": teams}
	if by != "" {
		payload["by"] = by
		payload["name"], payload["message"] = "done.task", "task t-1 completed"
	}
	return domain.Event{ID: id, Type: domain.EventEventEmitted, TenantID: "default", BotID: "bot-a", CorrelationID: "trace-1", CreatedAt: recorded, Payload: payload}
}

// operatorEvent is the operator's publication, as PublishGroupEvent
// records it.
func operatorEvent(id domain.EventID, team string) domain.Event {
	return domain.Event{ID: id, Type: domain.EventGroupEventPublished, TenantID: "default", GroupName: team, CreatedAt: recorded,
		Payload: map[string]any{"name": "order.shipped", "severity": "info", "message": "order o-1 shipped", "teams": []string{team}, "by": "operator"}}
}

// topicReader is an application's durable consumer of every topic.
func topicReader(t *testing.T, js jetstream.JetStream, name string) jetstream.Consumer {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		consumer, err := js.CreateOrUpdateConsumer(ctx, topicwire.TopicStream, jetstream.ConsumerConfig{Durable: name, FilterSubjects: []string{topicwire.TopicPrefix + ".>"}, AckPolicy: jetstream.AckExplicitPolicy})
		if err == nil {
			return consumer
		}
		if ctx.Err() != nil {
			t.Fatalf("consumer: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// readTopics reads want messages off the consumer, then makes sure no
// more come.
func readTopics(t *testing.T, consumer jetstream.Consumer, want int) []jetstream.Msg {
	t.Helper()
	var out []jetstream.Msg
	deadline := time.Now().Add(15 * time.Second)
	for len(out) < want && time.Now().Before(deadline) {
		batch, err := consumer.Fetch(want-len(out), jetstream.FetchMaxWait(500*time.Millisecond))
		if err != nil {
			t.Fatal(err)
		}
		for msg := range batch.Messages() {
			_ = msg.Ack()
			out = append(out, msg)
		}
	}
	if len(out) != want {
		t.Fatalf("read %d topics, want %d", len(out), want)
	}
	extra, err := consumer.Fetch(1, jetstream.FetchMaxWait(300*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	for msg := range extra.Messages() {
		t.Errorf("one more topic: %s %s", msg.Subject(), msg.Data())
	}
	return out
}

// A bot's event (one message per team), the operator's and the control
// plane's own each reach their subject once, with their author; a repeat
// is dropped by its id, an event of another kind is no topic, and an
// event the inner publisher did not record is not copied.
func TestEachTeamsEventReachesItsSubjectOnceWithItsAuthor(t *testing.T) {
	url := broker(t).ClientURL()
	topics := topicsOn(t, Config{URL: url})
	inner := &recording{}
	tap := topics.Tap(inner)
	ctx := context.Background()
	for _, event := range []domain.Event{
		botEvent("evt-1", "", "team-a", "team.b"),
		operatorEvent("evt-2", "team-a"),
		botEvent("evt-3", "control-plane", "team-a"),
		botEvent("evt-1", "", "team-a", "team.b"),
		{ID: "evt-4", Type: domain.EventTaskCompleted, TenantID: "default", CreatedAt: recorded},
	} {
		if err := tap.Publish(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	inner.fail = errors.New("store down")
	if err := tap.Publish(ctx, botEvent("evt-5", "", "team-a")); err == nil {
		t.Error("the inner publisher's failure was swallowed")
	}
	messages := readTopics(t, topicReader(t, operatorJS(t, url), "reader"), 4)
	want := []struct{ subject, team, publisher, bot string }{
		{"axlr.topic.default.team-a.order.placed", "team-a", "bot", "bot-a"},
		{"axlr.topic.default.team_b.order.placed", "team.b", "bot", "bot-a"},
		{"axlr.topic.default.team-a.order.shipped", "team-a", "operator", ""},
		{"axlr.topic.default.team-a.done.task", "team-a", "control-plane", "bot-a"},
	}
	for i, msg := range messages {
		dto, err := topicwire.DecodeTopic(msg.Data())
		if err != nil {
			t.Fatal(err)
		}
		if msg.Subject() != want[i].subject || dto.Team != want[i].team || dto.Publisher != want[i].publisher || dto.Bot != want[i].bot || dto.Tenant != "default" {
			t.Errorf("message %d: %s %+v, want %+v", i, msg.Subject(), dto, want[i])
		}
		if got := msg.Headers().Get(topicwire.MsgIDHeader); got != topicwire.TopicMsgID(dto.ID, dto.Team) {
			t.Errorf("message %d: Nats-Msg-Id %q", i, got)
		}
	}
	if len(inner.events) != 5 {
		t.Errorf("the inner publisher recorded %d events", len(inner.events))
	}
	// The streams the control plane defines, as the ADR has them.
	stream, err := operatorJS(t, url).Stream(ctx, topicwire.TopicStream)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := stream.Info(ctx)
	if info.Config.MaxAge != 24*time.Hour || info.Config.MaxBytes != 1<<30 || info.Config.Duplicates != 2*time.Minute || info.Config.Subjects[0] != "axlr.topic.>" {
		t.Errorf("AXLR_TOPIC = %+v", info.Config)
	}
	publish, err := operatorJS(t, url).Stream(ctx, topicwire.PublishStream)
	if err != nil {
		t.Fatal(err)
	}
	info, _ = publish.Info(ctx)
	if info.Config.MaxAge != 24*time.Hour || info.Config.Subjects[0] != "axlr.publish.>" {
		t.Errorf("AXLR_PUBLISH = %+v", info.Config)
	}
}

// JetStream drops a repeat by Nats-Msg-Id across the whole stream, not
// per subject: the reason an event's copy for each team carries the team
// in its key.
func TestTheBrokerDedupesAcrossSubjects(t *testing.T) {
	url := broker(t).ClientURL()
	topicsOn(t, Config{URL: url})
	js := operatorJS(t, url)
	ctx := context.Background()
	waitUntil(t, "the stream", func() bool { _, err := js.Stream(ctx, topicwire.TopicStream); return err == nil })
	for _, team := range []string{"team-a", "team-b"} {
		if _, err := js.Publish(ctx, topicwire.TopicSubject("default", team, "order.placed"), []byte(`{}`), jetstream.WithMsgID("evt-1")); err != nil {
			t.Fatal(err)
		}
	}
	stream, _ := js.Stream(ctx, topicwire.TopicStream)
	if info, err := stream.Info(ctx); err != nil || info.State.Msgs != 1 {
		t.Fatalf("one id on two subjects kept %d messages: %v", info.State.Msgs, err)
	}
	for _, team := range []string{"team-a", "team-b"} {
		if _, err := js.Publish(ctx, topicwire.TopicSubject("default", team, "order.placed"), []byte(`{}`), jetstream.WithMsgID(topicwire.TopicMsgID("evt-2", team))); err != nil {
			t.Fatal(err)
		}
	}
	if info, _ := stream.Info(ctx); info.State.Msgs != 3 {
		t.Errorf("the team's keys kept %d messages, want 3", info.State.Msgs)
	}
}

// A copy that cannot be written to the outbox is logged and never fails
// the event.
func TestACopyThatCannotBeWrittenDoesNotFailTheEvent(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var logs syncLog
	topics := topicsOn(t, Config{URL: "nats://127.0.0.1:1", Outbox: file, Log: log.New(&logs, "", 0)})
	if err := topics.Tap(&recording{}).Publish(context.Background(), botEvent("evt-1", "", "team-a")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "event evt-1 not copied") {
		t.Errorf("logs = %q", logs.String())
	}
}

// A copy made while the broker is away waits in the outbox; topics
// started again over it publish it once a broker answers.
func TestARestartRepublishesTheOutbox(t *testing.T) {
	s := broker(t)
	port := s.Addr().(*net.TCPAddr).Port
	url := s.ClientURL()
	s.Shutdown()
	s.WaitForShutdown()
	var logs syncLog
	cfg := Config{URL: url, Outbox: filepath.Join(t.TempDir(), "outbox-topics"), Log: log.New(&logs, "", 0)}
	topics := topicsOn(t, cfg)
	if err := topics.Tap(&recording{}).Publish(context.Background(), botEvent("evt-away", "", "team-a")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	left := topics.box.sweepFolder(topicOutboxFolder)
	if len(left) != 1 || left[0].msgID != "evt-away@team-a" || left[0].subject != "axlr.topic.default.team-a.order.placed" {
		t.Fatalf("outbox = %+v", left)
	}
	topics.Close()
	again, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: port, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go again.Start()
	if !again.ReadyForConnections(10 * time.Second) {
		t.Fatal("the broker did not start")
	}
	defer func() { again.Shutdown(); again.WaitForShutdown() }()
	restarted := topicsOn(t, cfg)
	messages := readTopics(t, topicReader(t, operatorJS(t, url), "reader"), 1)
	if !strings.Contains(string(messages[0].Data()), `"id":"evt-away"`) {
		t.Errorf("message = %s", messages[0].Data())
	}
	for deadline := time.Now().Add(5 * time.Second); len(restarted.box.sweepFolder(topicOutboxFolder)) > 0; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the outbox was not emptied")
		}
	}
	if !strings.Contains(logs.String(), "(retrying)") {
		t.Errorf("logs = %q", logs.String())
	}
}

// A copy older than the stream keeps it is dropped; one the broker
// refuses for good does not hold back the next.
func TestStaleAndRefusedCopiesAreDropped(t *testing.T) {
	url := brokerWith(t, func(o *server.Options) { o.MaxPayload = 2048 }).ClientURL()
	var logs syncLog
	dir := filepath.Join(t.TempDir(), "outbox-topics")
	stale, err := outbox{dir: dir}.addMessage(topicOutboxFolder, "axlr.topic.default.team-a.old", "evt-old@team-a", 1, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, topicOutboxFolder, "00000000000000000001-000000000001.json")
	if err := os.Rename(stale.path, old); err != nil {
		t.Fatal(err)
	}
	topics := topicsOn(t, Config{URL: url, Outbox: dir, Log: log.New(&logs, "", 0)})
	big := botEvent("evt-big", "", "team-a")
	big.Payload["message"] = strings.Repeat("x", 4096)
	tap := topics.Tap(&recording{})
	for _, event := range []domain.Event{big, botEvent("evt-good", "", "team-a")} {
		if err := tap.Publish(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	messages := readTopics(t, topicReader(t, operatorJS(t, url), "reader"), 1)
	if !strings.Contains(string(messages[0].Data()), `"id":"evt-good"`) {
		t.Errorf("message = %s", messages[0].Data())
	}
	if !strings.Contains(logs.String(), "evt-old@team-a dropped: older than") || !strings.Contains(logs.String(), "evt-big@team-a dropped: refused") {
		t.Errorf("logs = %q", logs.String())
	}
}

func TestTheTopicsNeedTheirBrokerAndOutbox(t *testing.T) {
	if _, err := NewTopics(Config{Outbox: t.TempDir()}); err == nil {
		t.Error("no URL")
	}
	if _, err := NewTopics(Config{URL: "nats://127.0.0.1:1"}); err == nil {
		t.Error("no outbox")
	}
	topics := topicsOn(t, Config{URL: "nats://127.0.0.1:1"})
	if err := topics.Listen(context.Background(), nil); err == nil {
		t.Error("a listener without a service")
	}
}

// service is a PublicationService over a team list that keeps what it
// is asked, and fails as told.
type service struct {
	mu        sync.Mutex
	groups    []application.Group
	published []application.PublishGroupEventCommand
	refused   []application.RefusedPublication
	// failures are how many publications fail as the store would; listFails
	// and auditFails the same for the teams and the audit.
	failures, listFails, auditFails int
}

func (s *service) ListGroups(context.Context) ([]application.Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listFails > 0 {
		s.listFails--
		return nil, errors.New("teams unavailable")
	}
	return s.groups, nil
}

func (s *service) PublishGroupEvent(_ context.Context, command application.PublishGroupEventCommand) (application.PublishAnswer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failures > 0 {
		s.failures--
		return application.PublishAnswer{}, errors.New("store unavailable")
	}
	if strings.HasPrefix(command.Name, "done.") {
		return application.PublishAnswer{}, domain.ValidationError{Field: "name", Reason: "done.* is the control plane's own"}
	}
	created := !slices.ContainsFunc(s.published, func(c application.PublishGroupEventCommand) bool { return c.IdempotencyKey == command.IdempotencyKey })
	s.published = append(s.published, command)
	return application.PublishAnswer{EventID: "evt-" + command.IdempotencyKey, Created: created}, nil
}

func (s *service) RefusePublication(_ context.Context, refused application.RefusedPublication) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.auditFails > 0 {
		s.auditFails--
		return errors.New("audit unavailable")
	}
	s.refused = append(s.refused, refused)
	return nil
}

func (s *service) counts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.published), len(s.refused)
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); !ok(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("waited for %s", what)
		}
	}
}

// publishOn publishes an application's publication on the broker, with
// its key unless it is empty.
func publishOn(t *testing.T, js jetstream.JetStream, subject, key, payload string) {
	t.Helper()
	msg := natsio.NewMsg(subject)
	msg.Data = []byte(payload)
	if key != "" {
		msg.Header.Set(topicwire.MsgIDHeader, key)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, err := js.PublishMsg(ctx, msg)
		if err == nil {
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("publish on %s: %v", subject, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func teams() []application.Group {
	return []application.Group{{Name: "team-a", TenantID: "default"}, {Name: "team.b", TenantID: "default"}, {Name: "team_b", TenantID: "default"}}
}

// A publication is published on its team as the operator, once: a repeat
// within the window is dropped by the broker, and one past it reaches
// the service again with the same key, which the control plane makes
// nothing of. A refused one (the rules, a bad envelope, no key, a team
// that is none or two) is taken off and audited, never given again.
func TestAPublicationIsPublishedOnceAndARefusedOneIsTakenOff(t *testing.T) {
	url := broker(t).ClientURL()
	var logs syncLog
	topics := topicsOn(t, Config{URL: url, TopicDuplicates: 500 * time.Millisecond, Log: log.New(&logs, "", 0)})
	svc := &service{groups: teams()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := topics.Listen(ctx, svc); err != nil {
		t.Fatal(err)
	}
	js := operatorJS(t, url)
	good := `{"name":"order.placed","message":"order o-1 placed","data":{"order":"o-1"},"to":["bot-b"]}`
	publishOn(t, js, "axlr.publish.default.team-a.order.placed", "k-1", good)
	publishOn(t, js, "axlr.publish.default.team-a.order.placed", "k-1", good)
	waitUntil(t, "the publication", func() bool { n, _ := svc.counts(); return n == 1 })
	command := svc.published[0]
	if command.Group != "team-a" || command.Tenant != "default" || command.IdempotencyKey != "k-1" || command.Name != "order.placed" || command.Data["order"] != "o-1" || command.To[0] != "bot-b" {
		t.Errorf("command = %+v", command)
	}
	time.Sleep(700 * time.Millisecond)
	publishOn(t, js, "axlr.publish.default.team-a.order.placed", "k-1", good)
	waitUntil(t, "the repeat past the window", func() bool { n, _ := svc.counts(); return n == 2 })
	if svc.published[1].IdempotencyKey != "k-1" || !strings.Contains(logs.String(), "publication k-1 on default/team-a repeats evt-k-1: nothing delivered again") {
		t.Errorf("repeat = %+v, logs %q", svc.published[1], logs.String())
	}

	for _, bad := range []struct{ subject, key, payload string }{
		{"axlr.publish.default.team-a.done.task", "k-2", `{"name":"done.task","message":"m"}`},
		{"axlr.publish.default.team-a.order.placed", "k-3", `{`},
		{"axlr.publish.default.team-a.order.placed", "", good},
		{"axlr.publish.default.team-z.order.placed", "k-4", good},
		{"axlr.publish.default.team_b.order.placed", "k-5", good},
	} {
		publishOn(t, js, bad.subject, bad.key, bad.payload)
	}
	waitUntil(t, "five refusals", func() bool { _, n := svc.counts(); return n == 5 })
	time.Sleep(300 * time.Millisecond)
	if n, refusals := svc.counts(); n != 2 || refusals != 5 {
		t.Errorf("published %d, refused %d", n, refusals)
	}
	reasons := []string{"control plane's own", "topics:", "idempotency key", "not found", "2 teams share"}
	for i, refusal := range svc.refused {
		if !strings.Contains(refusal.Reason, reasons[i]) {
			t.Errorf("refusal %d = %+v", i, refusal)
		}
	}
	if svc.refused[0].Team != "team-a" || svc.refused[0].Tenant != "default" || svc.refused[0].Key != "k-2" || svc.refused[0].Name != "done.task" {
		t.Errorf("refusal = %+v", svc.refused[0])
	}
	if !strings.Contains(logs.String(), "refused and taken off") {
		t.Errorf("logs = %q", logs.String())
	}
	// Taken off for good: nothing waits for the consumer.
	consumer, err := js.Consumer(ctx, topicwire.PublishStream, topicwire.ControlPlaneConsumer)
	if err != nil {
		t.Fatal(err)
	}
	info, err := consumer.Info(ctx)
	if err != nil || info.NumAckPending != 0 || info.NumPending != 0 {
		t.Errorf("consumer = %+v %v", info, err)
	}
}

// A publication the store failed is given again until it is published;
// so is one whose team list or refusal audit failed.
func TestAFailedPublicationIsGivenAgain(t *testing.T) {
	url := broker(t).ClientURL()
	var logs syncLog
	topics := topicsOn(t, Config{URL: url, Log: log.New(&logs, "", 0)})
	svc := &service{groups: teams(), failures: 2, listFails: 1, auditFails: 1}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := topics.Listen(ctx, svc); err != nil {
		t.Fatal(err)
	}
	js := operatorJS(t, url)
	publishOn(t, js, "axlr.publish.default.team-a.order.placed", "k-1", `{"name":"order.placed","message":"m"}`)
	waitUntil(t, "the publication after the failures", func() bool { n, _ := svc.counts(); return n == 1 })
	publishOn(t, js, "axlr.publish.default.team-a.done.task", "k-2", `{"name":"done.task","message":"m"}`)
	waitUntil(t, "the refusal after its audit failed", func() bool { _, n := svc.counts(); return n == 1 })
	for _, want := range []string{"teams unavailable (again in 250ms)", "store unavailable (again in 500ms)", "store unavailable (again in 1s)", "auditing its refusal: audit unavailable"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("no %q in %q", want, logs.String())
		}
	}
}

// The consumer is lost with the broker and found again.
func TestTheConsumerComesBackWithTheBroker(t *testing.T) {
	dir := t.TempDir()
	s := brokerWith(t, func(o *server.Options) { o.StoreDir = dir })
	port := s.Addr().(*net.TCPAddr).Port
	url := s.ClientURL()
	topics := topicsOn(t, Config{URL: url})
	svc := &service{groups: teams()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := topics.Listen(ctx, svc); err != nil {
		t.Fatal(err)
	}
	publishOn(t, operatorJS(t, url), "axlr.publish.default.team-a.order.placed", "k-1", `{"name":"order.placed","message":"m"}`)
	waitUntil(t, "the first publication", func() bool { n, _ := svc.counts(); return n == 1 })
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
	publishOn(t, operatorJS(t, url), "axlr.publish.default.team-a.order.placed", "k-2", `{"name":"order.placed","message":"m"}`)
	waitUntil(t, "the publication after the broker came back", func() bool { n, _ := svc.counts(); return n == 2 })
}

func TestRefusedForGoodAndTheDelay(t *testing.T) {
	for err, want := range map[error]bool{
		domain.ValidationError{Field: "f"}:           true,
		domain.NotFoundError{Kind: "group", ID: "x"}: true,
		domain.ForbiddenError{}:                      true,
		domain.ConflictError{}:                       true,
		domain.StateError{}:                          true,
		errors.New("disk full"):                      false,
		context.DeadlineExceeded:                     false,
	} {
		if refusedForGood(err) != want {
			t.Errorf("%v: %v", err, !want)
		}
	}
	if _, _, err := resolve(teams(), topicwire.Address{TenantToken: "default", TeamToken: "team-a"}); err != nil {
		t.Error(err)
	}
}
