package unixapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func TestGroupsOverTheSocket(t *testing.T) {
	client, _, _ := serve(t, "complete")
	ctx := context.Background()
	for _, name := range []string{"watcher", "worker-1"} {
		d := definition(name, "observe")
		d.Groups = []string{"ops"}
		if _, err := client.RegisterBot(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	groups, err := client.ListGroups(ctx)
	if err != nil || len(groups) != 1 || groups[0].Name != "ops" || len(groups[0].Members) != 2 {
		t.Fatalf("groups = %+v %v", groups, err)
	}
	group, err := client.GetGroup(ctx, "ops", "ops")
	if err != nil || group.TenantID != "ops" || group.Members[0].BotID != "watcher" {
		t.Fatalf("group = %+v %v", group, err)
	}
	if _, err := client.GetGroup(ctx, "sre", ""); !domain.IsNotFound(err) {
		t.Errorf("missing group: %v", err)
	}
	bot, err := client.GetBot(ctx, "worker-1")
	if err != nil || len(bot.Groups) != 1 {
		t.Errorf("bot groups = %+v %v", bot.Groups, err)
	}
}

func TestEventsOverTheSocket(t *testing.T) {
	client, _, _ := serve(t, "completed")
	ctx := context.Background()
	if _, err := client.Register(ctx, application.NodeRegistration{ID: "web-1", HostName: "web-1", Capabilities: domain.NodeCapabilities{PolicySupported: true}}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"watcher", "worker-1"} {
		d := definition(name, "observe")
		d.Groups = []string{"ops"}
		if _, err := client.RegisterBot(ctx, d); err != nil {
			t.Fatal(err)
		}
		if _, err := client.EnableBot(ctx, name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.SubmitTask(ctx, SubmitRequest{BotID: "watcher", Objective: "Warn worker-1", IdempotencyKey: "k-1"}); err != nil {
		t.Fatal(err)
	}
	assignment, found, err := client.NextAssignment(ctx, "web-1", 5*time.Second)
	if err != nil || !found || assignment.Run.Events == nil || assignment.Run.Events.Teammates[0] != "worker-1" {
		t.Fatalf("assignment = %+v %v %v", assignment.Run, found, err)
	}
	before, err := client.ListGroupEvents(ctx, application.GroupEventsQuery{Name: "ops", Tenant: "ops", Limit: 1000})
	if err != nil || len(before) == 0 {
		t.Fatalf("timeline = %v %v", len(before), err)
	}
	last := before[len(before)-1].ID.String()
	answer, err := client.Emit(ctx, "web-1", assignment.ExecutionID, application.EmitEventCommand{Name: "warn.deploy-slow", Message: "a deploy is starting", Severity: "info", To: []string{"worker-1"}})
	if err != nil || answer.Refused != nil || len(answer.Deliveries) != 1 || answer.Severity != domain.SeverityWarning {
		t.Fatalf("emit = %+v %v", answer, err)
	}
	if refused, err := client.Emit(ctx, "web-1", assignment.ExecutionID, application.EmitEventCommand{Name: "error.bot", Message: "x"}); err != nil || refused.Refused == nil || refused.Refused.Reason != domain.RefusedNotAllowed {
		t.Errorf("error.bot = %+v %v", refused, err)
	}
	if _, err := client.Emit(ctx, "web-1", assignment.ExecutionID, application.EmitEventCommand{Name: "Bad", Message: "x"}); err == nil {
		t.Error("a bad name was taken")
	}
	after, err := client.ListGroupEvents(ctx, application.GroupEventsQuery{Name: "ops", After: last})
	if err != nil || len(after) == 0 || after[0].Type == "" {
		t.Errorf("after = %+v %v", after, err)
	}
	if _, err := client.ListGroupEvents(ctx, application.GroupEventsQuery{Name: "sre"}); !domain.IsNotFound(err) {
		t.Errorf("missing team: %v", err)
	}
	// The bot asks to hear errors; it is pending.
	asked, err := client.Subscribe(ctx, "web-1", assignment.ExecutionID, application.SubscribeCommand{Topic: "error.*", Why: "to retry"})
	if err != nil || asked.Status != "pending" || asked.Refused != nil {
		t.Errorf("ask = %+v %v", asked, err)
	}
	if again, err := client.Subscribe(ctx, "web-1", assignment.ExecutionID, application.SubscribeCommand{Topic: "error.*", Why: "to retry"}); err != nil || again.Refused == nil || again.Refused.Reason != domain.RefusedAlreadySubscribed {
		t.Errorf("again = %+v %v", again, err)
	}
	if bot, _ := client.GetBot(ctx, "watcher"); len(bot.Subscriptions) != 1 || bot.Subscriptions[0].Status != "pending" {
		t.Errorf("watcher = %+v", bot.Subscriptions)
	}
	// The operator publishes; a repeat with the key is the same event.
	stop, err := client.PublishGroupEvent(ctx, application.PublishGroupEventCommand{Group: "ops", Tenant: "ops", Name: "stop.all", Message: "Everyone stop", IdempotencyKey: "p-1", Data: map[string]any{"why": "a storm"}})
	if err != nil || !stop.Created || len(stop.Deliveries) != 2 {
		t.Fatalf("publish = %+v %v", stop, err)
	}
	if again, err := client.PublishGroupEvent(ctx, application.PublishGroupEventCommand{Group: "ops", Name: "stop.all", Message: "Everyone stop", IdempotencyKey: "p-1", Data: map[string]any{"why": "a storm"}}); err != nil || again.Created || again.EventID != stop.EventID {
		t.Errorf("again = %+v %v", again, err)
	}
	if _, err := client.PublishGroupEvent(ctx, application.PublishGroupEventCommand{Group: "ops", Name: "error.bot", Message: "x", IdempotencyKey: "p-2"}); err == nil || !strings.Contains(err.Error(), domain.RefusedNotAllowed) {
		t.Errorf("error.bot: %v", err)
	}
	topics, err := client.ListGroupTopics(ctx, "ops", "ops")
	names := []string{}
	for _, topic := range topics {
		names = append(names, topic.Name)
	}
	if err != nil || strings.Join(names, ",") != "error.*,stop.all,warn.deploy-slow" {
		t.Errorf("topics = %v %v", names, err)
	}
}

func TestTheTeamTimelineWaitsForTheNextEvent(t *testing.T) {
	_, local, dir := serve(t, "completed")
	ctx := context.Background()
	d := definition("worker-1", "observe")
	d.Groups = []string{"ops"}
	if _, err := local.RegisterBot(ctx, d); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(local).OperatorHandler("secret-token-0123456789"))
	defer server.Close()
	get := func(query string) (int, []domain.Event, time.Duration) {
		request, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/groups/ops/events"+query, nil)
		request.Header.Set("Authorization", "Bearer secret-token-0123456789")
		started := time.Now()
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var events []domain.Event
		_ = json.NewDecoder(response.Body).Decode(&events)
		return response.StatusCode, events, time.Since(started)
	}
	code, events, _ := get("")
	if code != http.StatusOK || len(events) == 0 {
		t.Fatalf("timeline = %d %d", code, len(events))
	}
	last := events[len(events)-1].ID.String()
	code, events, took := get("?after=" + last + "&wait=1200ms")
	if code != http.StatusOK || len(events) != 0 || took < time.Second {
		t.Errorf("empty wait = %d %d %s", code, len(events), took)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		_, _ = local.EnableBot(ctx, "worker-1")
	}()
	code, events, took = get("?after=" + last + "&wait=10s")
	if code != http.StatusOK || len(events) != 1 || events[0].Type != domain.EventBotEnabled || took > 5*time.Second {
		t.Errorf("woken wait = %d %+v %s", code, events, took)
	}
	for _, bad := range []string{"?wait=soon", "?limit=x"} {
		if code, _, _ := get(bad); code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, code)
		}
	}
	_ = dir
}

func TestStopAndUrgentOverTheSocket(t *testing.T) {
	client, _, _ := serve(t, "completed")
	ctx := context.Background()
	if _, err := client.RegisterBot(ctx, definition("worker-1", "observe")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "worker-1"); err != nil {
		t.Fatal(err)
	}
	first, err := client.SubmitTask(ctx, SubmitRequest{BotID: "worker-1", Objective: "Watch", IdempotencyKey: "k-1"})
	if err != nil {
		t.Fatal(err)
	}
	urgent, err := client.SubmitTask(ctx, SubmitRequest{BotID: "worker-1", Objective: "Roll back", IdempotencyKey: "k-2", Priority: "urgent"})
	if err != nil || urgent.Task.Priority != "urgent" {
		t.Fatalf("urgent = %+v %v", urgent.Task, err)
	}
	stopped, err := client.StopBot(ctx, "worker-1")
	if err != nil || stopped.Bot.Status != "Paused" || len(stopped.Cancelled) != 2 || len(stopped.NotCancelled) != 0 {
		t.Fatalf("stop = %+v %v", stopped, err)
	}
	if task, _ := client.GetTask(ctx, first.Task.ID); task.Status != "Cancelled" || !strings.HasPrefix(task.FailureReason, "stopped by ") {
		t.Errorf("first = %+v", task)
	}
	if _, err := client.StopBot(ctx, "ghost"); !domain.IsNotFound(err) {
		t.Errorf("ghost: %v", err)
	}
	if _, err := client.StopBot(ctx, "Bad Id"); err == nil {
		t.Error("a bad id was stopped")
	}
}
