package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	natsio "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	natsbus "github.com/underpass-ai/axlr-bots/adapters/bus/nats"
	"github.com/underpass-ai/axlr-bots/adapters/bus/topicwire"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// broker starts a NATS server with JetStream for one test.
func broker(t *testing.T) *server.Server {
	t.Helper()
	s, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go s.Start()
	if !s.ReadyForConnections(10 * time.Second) {
		t.Fatal("the broker did not start")
	}
	t.Cleanup(func() { s.Shutdown(); s.WaitForShutdown() })
	return s
}

// application is a client of the broker as an application is.
func applicationJS(t *testing.T, url string) jetstream.JetStream {
	t.Helper()
	conn, err := natsio.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	js, err := jetstream.New(conn)
	if err != nil {
		t.Fatal(err)
	}
	return js
}

// team registers a team of two: bot-a, which a node runs, and bot-b,
// which hears every order and every task done.
func team(t *testing.T, client *unixapi.Client) {
	t.Helper()
	ctx := context.Background()
	for _, d := range []application.BotDefinition{
		{Tenant: "default", Name: "bot-a", Owner: "o", Mission: "m", Groups: []string{"team-a"}},
		{Tenant: "default", Name: "bot-b", Owner: "o", Mission: "m", Groups: []string{"team-a"}, Subscriptions: []domain.SubscriptionSnapshot{{Topic: "order.*"}, {Topic: "done.*"}}},
	} {
		if _, err := client.RegisterBot(ctx, d); err != nil {
			t.Fatal(err)
		}
		if _, err := client.EnableBot(ctx, d.Name); err != nil {
			t.Fatal(err)
		}
	}
}

// topicsDaemon starts the daemon with its topics on the broker.
func topicsDaemon(t *testing.T, f *fixture, url string) (*unixapi.Client, func()) {
	t.Helper()
	topics, err := natsbus.NewTopics(natsbus.Config{URL: url, Outbox: filepath.Join(f.state, "outbox", "topics"), Name: "test", TopicDuplicates: 500 * time.Millisecond, Log: log.New(f.logs, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	// No reconciliation while the test plays the node: the fixture's grace
	// would fail its running task before it reports.
	client, stop := f.startWith(t, func(o *Options) { o.Topics, o.Grace, o.ReconcileEvery = topics, time.Hour, time.Hour })
	return client, func() { stop(); topics.Close() }
}

// topicsOf reads every topic the broker holds, waiting for want.
func topicsOf(t *testing.T, js jetstream.JetStream, want int) []topicwire.TopicEventDTO {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var out []topicwire.TopicEventDTO
	for len(out) < want && ctx.Err() == nil {
		stream, err := js.Stream(ctx, topicwire.TopicStream)
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		info, _ := stream.Info(ctx)
		out = out[:0]
		for seq := uint64(1); seq <= info.State.LastSeq; seq++ {
			msg, err := stream.GetMsg(ctx, seq)
			if err != nil {
				continue
			}
			dto, err := topicwire.DecodeTopic(msg.Data)
			if err != nil {
				t.Fatal(err)
			}
			if msg.Subject != topicwire.TopicSubject(dto.Tenant, dto.Team, dto.Name) {
				t.Errorf("%s carries %+v", msg.Subject, dto)
			}
			out = append(out, dto)
		}
		if len(out) < want {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if len(out) != want {
		t.Fatalf("the topics hold %d events, want %d: %+v", len(out), want, out)
	}
	return out
}

// A bot's event, the operator's publication and the control plane's
// done.task each reach the team's subject once, with their author.
func TestEachTeamEventReachesItsTopicOnceWithItsAuthor(t *testing.T) {
	url := broker(t).ClientURL()
	f := newFixture(t, "completed")
	client, stop := topicsDaemon(t, f, url)
	defer stop()
	ctx := context.Background()
	team(t, client)
	if _, err := client.Register(ctx, application.NodeRegistration{ID: "web-1", HostName: "web-1"}); err != nil {
		t.Fatal(err)
	}
	beating, stopBeating := context.WithCancel(ctx)
	defer stopBeating()
	go func() {
		for beating.Err() == nil {
			_, _ = client.Heartbeat(beating, "web-1")
			select {
			case <-beating.Done():
			case <-time.After(30 * time.Millisecond):
			}
		}
	}()
	submitted, err := client.SubmitTask(ctx, unixapi.SubmitRequest{BotID: "bot-a", Objective: "place the order", IdempotencyKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	assignment, found, err := client.NextAssignment(ctx, "web-1", 5*time.Second)
	if err != nil || !found || assignment.Run.TaskID != domain.TaskID(submitted.Task.ID) {
		t.Fatalf("assignment: %v %v", err, found)
	}
	emitted, err := client.Emit(ctx, "web-1", assignment.ExecutionID, application.EmitEventCommand{Name: "order.placed", Message: "order o-1 placed", Data: json.RawMessage(`{"order":"o-1"}`)})
	if err != nil || emitted.Refused != nil {
		t.Fatalf("emit = %+v %v", emitted, err)
	}
	if _, err := client.PublishGroupEvent(ctx, application.PublishGroupEventCommand{Group: "team-a", Name: "order.shipped", Message: "order o-1 shipped", IdempotencyKey: "k-api"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Report(ctx, "web-1", assignment.ExecutionID, application.ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "placed"}}); err != nil {
		t.Fatal(err)
	}
	topics := topicsOf(t, applicationJS(t, url), 3)
	want := []struct{ name, publisher, bot string }{{"order.placed", "bot", "bot-a"}, {"order.shipped", "operator", ""}, {"done.task", "control-plane", "bot-a"}}
	timeline, err := client.ListGroupEvents(ctx, application.GroupEventsQuery{Name: "team-a", Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	for i, topic := range topics {
		if topic.Name != want[i].name || topic.Publisher != want[i].publisher || topic.Bot != want[i].bot || topic.Team != "team-a" || topic.Tenant != "default" {
			t.Errorf("topic %d = %+v, want %+v", i, topic, want[i])
		}
		if !slices.ContainsFunc(timeline, func(e domain.Event) bool { return e.ID.String() == topic.ID }) {
			t.Errorf("topic %d's id %s is not on the team's timeline", i, topic.ID)
		}
	}
	if topics[0].Data["order"] != "o-1" {
		t.Errorf("data = %+v", topics[0].Data)
	}
}

// deliveriesOf is who heard the operator's publication of the key, and
// how, as the team's timeline records it.
func deliveriesOf(t *testing.T, timeline []domain.Event, eventID string) []string {
	t.Helper()
	for _, event := range timeline {
		if event.Type != domain.EventGroupEventPublished || event.Payload["eventId"] != eventID {
			continue
		}
		var deliveries []struct {
			BotID string `json:"botId"`
			Mode  string `json:"mode"`
		}
		raw, _ := json.Marshal(event.Payload["deliveries"])
		_ = json.Unmarshal(raw, &deliveries)
		var out []string
		for _, d := range deliveries {
			out = append(out, d.BotID+"/"+d.Mode)
		}
		return out
	}
	t.Fatalf("no publication %s on the timeline", eventID)
	return nil
}

func published(timeline []domain.Event) int {
	n := 0
	for _, event := range timeline {
		if event.Type == domain.EventGroupEventPublished {
			n++
		}
	}
	return n
}

// publishOn publishes as an application does.
func publishOn(t *testing.T, js jetstream.JetStream, key string, p topicwire.PublicationDTO) {
	t.Helper()
	message, err := topicwire.EncodePublication("default", "team-a", key, p)
	if err != nil {
		t.Fatal(err)
	}
	msg := natsio.NewMsg(message.Subject)
	msg.Data, msg.Header = message.Payload, natsio.Header{}
	msg.Header.Set(topicwire.MsgIDHeader, message.MsgID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		if _, err := js.PublishMsg(ctx, msg); err == nil {
			return
		} else if ctx.Err() != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A publication on the team's subject makes the deliveries POST makes; a
// repeat of its key, inside the broker's window or past it, makes none;
// a refused one is taken off and audited.
func TestAnInboundPublicationMakesTheDeliveriesPostMakes(t *testing.T) {
	url := broker(t).ClientURL()
	f := newFixture(t, "completed")
	client, stop := topicsDaemon(t, f, url)
	defer stop()
	ctx := context.Background()
	team(t, client)
	body := topicwire.PublicationDTO{Name: "order.shipped", Message: "order o-1 shipped", Data: map[string]any{"order": "o-1"}}
	posted, err := client.PublishGroupEvent(ctx, application.PublishGroupEventCommand{Group: "team-a", Name: body.Name, Message: body.Message, Data: body.Data, IdempotencyKey: "k-post"})
	if err != nil || len(posted.Deliveries) == 0 {
		t.Fatalf("posted = %+v %v", posted, err)
	}
	js := applicationJS(t, url)
	publishOn(t, js, "k-nats", body)
	publishOn(t, js, "k-nats", body)
	var timeline []domain.Event
	for deadline := time.Now().Add(15 * time.Second); published(timeline) < 2; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the publication never reached the team: %s", f.logs.String())
		}
		if timeline, err = client.ListGroupEvents(ctx, application.GroupEventsQuery{Name: "team-a", Limit: 1000}); err != nil {
			t.Fatal(err)
		}
	}
	var viaNATS string
	for _, event := range timeline {
		if id, _ := event.Payload["eventId"].(string); event.Type == domain.EventGroupEventPublished && id != posted.EventID {
			viaNATS = id
		}
	}
	if got, want := deliveriesOf(t, timeline, viaNATS), deliveriesOf(t, timeline, posted.EventID); !slices.Equal(got, want) || len(got) == 0 {
		t.Errorf("over NATS %v, over POST %v", got, want)
	}
	// Past the broker's window the repeat reaches the control plane, whose
	// key makes nothing of it.
	time.Sleep(700 * time.Millisecond)
	publishOn(t, js, "k-nats", body)
	refused := topicwire.PublicationDTO{Name: "done.task", Message: "forged"}
	publishOn(t, js, "k-done", refused)
	waitForLog(t, f, `publication "k-done" on axlr.publish.default.team-a.done.task refused and taken off`)
	waitForLog(t, f, "publication k-nats on default/team-a repeats "+viaNATS+": nothing delivered again")
	if timeline, err = client.ListGroupEvents(ctx, application.GroupEventsQuery{Name: "team-a", Limit: 1000}); err != nil || published(timeline) != 2 {
		t.Errorf("the timeline holds %d publications after the repeats: %v", published(timeline), err)
	}
	if published, repeats := strings.Count(f.logs.String(), "publication k-nats on default/team-a published"), strings.Count(f.logs.String(), "publication k-nats on default/team-a repeats"); published != 1 || repeats != 1 {
		t.Errorf("the control plane published k-nats %d times and saw %d repeats, want 1 and 1 (one dropped by the broker)", published, repeats)
	}
	audit, err := os.ReadFile(filepath.Join(f.state, "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var denied bool
	for _, line := range strings.Split(strings.TrimSpace(string(audit)), "\n") {
		var entry domain.AuditEntry
		if json.Unmarshal([]byte(line), &entry) == nil && entry.Action == "event.publish" && entry.Outcome == domain.AuditDenied && entry.Details["key"] == "k-done" && entry.Subject == "team-a" {
			denied = true
		}
	}
	if !denied {
		t.Errorf("no event.publish denied in the audit:\n%s", audit)
	}
}

// Without the topics nothing is published nor read: the broker never
// sees the control plane.
func TestWithTopicsOffNothingIsPublished(t *testing.T) {
	url := broker(t).ClientURL()
	f := newFixture(t, "completed")
	client, stop := f.start(t)
	defer stop()
	ctx := context.Background()
	team(t, client)
	if _, err := client.PublishGroupEvent(ctx, application.PublishGroupEventCommand{Group: "team-a", Name: "order.shipped", Message: "m", IdempotencyKey: "k"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	js := applicationJS(t, url)
	for _, stream := range []string{topicwire.TopicStream, topicwire.PublishStream} {
		if _, err := js.Stream(ctx, stream); !errors.Is(err, jetstream.ErrStreamNotFound) {
			t.Errorf("stream %s: %v", stream, err)
		}
	}
}
