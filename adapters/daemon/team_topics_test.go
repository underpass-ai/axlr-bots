package daemon

import (
	"context"
	"encoding/json"
	"log"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	natsbus "github.com/underpass-ai/axlr-bots/adapters/bus/nats"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// natsBroker starts a broker with JetStream for one test.
func natsBroker(t *testing.T) string {
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
	return s.ClientURL()
}

// A node's execution of a team-b bot emits order.new through the node
// route; a task appears for team-a's bot, on the bus in process (within the
// emit, never through the runner's lock again) and on NATS (through the
// runner, from the consumer's goroutine). Run with -race.
func TestADomainEventCrossesThroughTheDaemon(t *testing.T) {
	for name, bus := range map[string]func(t *testing.T, f *fixture) func(*Options){
		"direct": func(*testing.T, *fixture) func(*Options) { return nil },
		"nats": func(t *testing.T, f *fixture) func(*Options) {
			domainBus, err := natsbus.NewDomainBus(natsbus.Config{URL: natsBroker(t), Outbox: filepath.Join(f.dir, "outbox-domain"), Name: "test", Log: log.New(f.logs, "", 0)})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(domainBus.Close)
			return func(o *Options) { o.DomainBus = domainBus }
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, "completed")
			withBus := bus(t, f)
			client, stop := f.startWith(t, func(o *Options) {
				// The team-b task is the node's while the test emits: the
				// fixture's 1 ms grace would fail it under a slow runner.
				o.Grace, o.ReconcileEvery = time.Hour, time.Hour
				if withBus != nil {
					withBus(o)
				}
			})
			defer stop()
			crossesThroughTheNodeRoute(t, f, client)
		})
	}
}

func crossesThroughTheNodeRoute(t *testing.T, f *fixture, client *unixapi.Client) {
	t.Helper()
	ctx := context.Background()
	teamB := application.BotDefinition{Tenant: "default", Name: "b-triage", Owner: "o", Mission: "m", Groups: []string{"team-b"},
		Events: &domain.EventsSnapshot{Emit: []domain.EmitRuleSnapshot{{Name: "order.new", Teams: []string{"team-a"}}}}}
	teamA := application.BotDefinition{Tenant: "default", Name: "a-fixer", Owner: "o", Mission: "m", Groups: []string{"team-a"},
		Subscriptions: []domain.SubscriptionSnapshot{{Topic: "order.new", Team: "team-b", From: []string{"b-triage"}, Mode: "task", Deliver: "team"}}}
	for _, d := range []application.BotDefinition{teamB, teamA} {
		registered, err := client.RegisterBot(ctx, d)
		if err != nil {
			t.Fatal(err)
		}
		// The definition keeps the fields across.
		if d.Name == "b-triage" && (registered.Events == nil || len(registered.Events.Emit[0].Teams) != 1) {
			t.Errorf("events = %+v", registered.Events)
		}
		if d.Name == "a-fixer" && (registered.Subscriptions[0].Team != "team-b" || registered.Subscriptions[0].From[0] != "b-triage") {
			t.Errorf("subscriptions = %+v", registered.Subscriptions)
		}
		if _, err := client.EnableBot(ctx, d.Name); err != nil {
			t.Fatal(err)
		}
	}
	// A PATCH of the subscription naming team-b's bot in from is kept as is.
	subs := []domain.SubscriptionSnapshot{{Topic: "order.new", Team: "team-b", From: []string{"b-triage"}, Mode: "task", Deliver: "team"}}
	if patched, err := client.UpdateBot(ctx, "a-fixer", application.BotPatch{Subscriptions: &subs}); err != nil || patched.Subscriptions[0].Team != "team-b" || patched.Subscriptions[0].From[0] != "b-triage" {
		t.Fatalf("patched = %+v %v", patched.Subscriptions, err)
	}
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
	if _, err := client.SubmitTask(ctx, unixapi.SubmitRequest{BotID: "b-triage", Objective: "Triage", IdempotencyKey: "k-b"}); err != nil {
		t.Fatal(err)
	}
	assignment, found, err := client.NextAssignment(ctx, "web-1", 2*time.Second)
	if err != nil || !found || assignment.Run == nil || assignment.Run.BotID != "b-triage" {
		t.Fatalf("assignment = %+v %v %v", assignment.Run, found, err)
	}
	answer, err := client.Emit(ctx, "web-1", assignment.ExecutionID, application.EmitEventCommand{Name: "order.new", Message: "a finding", Data: json.RawMessage(`{"id": "h-1", "context": "project:team-b.item.h-1"}`)})
	if err != nil || len(answer.Deliveries) != 1 || !answer.Deliveries[0].Crossed || answer.Deliveries[0].Team != "team-a" {
		t.Fatalf("answer = %+v %v", answer, err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		tasks, err := client.ListTasks(ctx, application.TaskQuery{BotID: "a-fixer"})
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) == 1 {
			if tasks[0].Origin == nil || tasks[0].Origin.Crossings != 1 || tasks[0].Origin.Team != "team-b" || tasks[0].Delegation != nil {
				t.Errorf("task = %+v", tasks[0])
			}
			break
		}
		if len(tasks) > 1 || time.Now().After(deadline) {
			t.Fatalf("team-a's tasks = %d", len(tasks))
		}
		time.Sleep(20 * time.Millisecond)
	}
	// The same emit again is the same event: nothing new crosses.
	again, err := client.Emit(ctx, "web-1", assignment.ExecutionID, application.EmitEventCommand{Name: "order.new", Message: "a finding", Data: json.RawMessage(`{"id": "h-1", "context": "project:team-b.item.h-1"}`)})
	if err != nil || !again.Deduplicated {
		t.Errorf("again = %+v %v\n%s", again, err, f.logs.String())
	}
	if tasks, _ := client.ListTasks(ctx, application.TaskQuery{BotID: "a-fixer"}); len(tasks) != 1 {
		t.Errorf("team-a's tasks = %d", len(tasks))
	}
}
