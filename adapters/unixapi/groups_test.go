package unixapi

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func TestGroupsOverTheSocket(t *testing.T) {
	_, local, _ := serve(t, "complete")
	ctx := context.Background()
	for _, name := range []string{"watcher", "worker-1"} {
		d := definition(name, "observe")
		d.Groups = []string{"ops"}
		if _, err := local.RegisterBot(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	groups, err := local.ListGroups(ctx)
	if err != nil || len(groups) != 1 || groups[0].Name != "ops" || len(groups[0].Members) != 2 {
		t.Fatalf("groups = %+v %v", groups, err)
	}
	group, err := local.GetGroup(ctx, "ops", "ops")
	if err != nil || group.TenantID != "ops" || group.Members[0].BotID != "watcher" {
		t.Fatalf("group = %+v %v", group, err)
	}
	if _, err := local.GetGroup(ctx, "platform", ""); !domain.IsNotFound(err) {
		t.Errorf("missing group: %v", err)
	}
	bot, err := local.GetBot(ctx, "worker-1")
	if err != nil || len(bot.Groups) != 1 {
		t.Errorf("bot groups = %+v %v", bot.Groups, err)
	}
}

func TestEventsOverTheSocket(t *testing.T) {
	client, local, _ := serve(t, "completed")
	ctx := context.Background()
	if _, err := client.Register(ctx, application.NodeRegistration{ID: "web-1", HostName: "web-1", Capabilities: domain.NodeCapabilities{PolicySupported: true}}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"watcher", "worker-1"} {
		d := definition(name, "observe")
		d.Groups = []string{"ops"}
		if _, err := local.RegisterBot(ctx, d); err != nil {
			t.Fatal(err)
		}
		if _, err := local.EnableBot(ctx, name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := local.SubmitTask(ctx, application.SubmitRequest{BotID: "watcher", Objective: "Warn worker-1", IdempotencyKey: "k-1"}); err != nil {
		t.Fatal(err)
	}
	assignment, found, err := client.NextAssignment(ctx, "web-1", 5*time.Second)
	if err != nil || !found || assignment.Run.Events == nil || assignment.Run.Events.Teammates[0] != "worker-1" {
		t.Fatalf("assignment = %+v %v %v", assignment.Run, found, err)
	}
	before, err := local.ListGroupEvents(ctx, application.GroupEventsQuery{Name: "ops", Tenant: "ops", Limit: 1000})
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
	after, err := local.ListGroupEvents(ctx, application.GroupEventsQuery{Name: "ops", After: last})
	if err != nil || len(after) == 0 || after[0].Type == "" {
		t.Errorf("after = %+v %v", after, err)
	}
	if _, err := local.ListGroupEvents(ctx, application.GroupEventsQuery{Name: "platform"}); !domain.IsNotFound(err) {
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
	if bot, _ := local.GetBot(ctx, "watcher"); len(bot.Subscriptions) != 1 || bot.Subscriptions[0].Status != "pending" {
		t.Errorf("watcher = %+v", bot.Subscriptions)
	}
	// The operator publishes; a repeat with the key is the same event.
	stop, err := local.PublishGroupEvent(ctx, application.PublishGroupEventCommand{Group: "ops", Tenant: "ops", Name: "stop.all", Message: "Everyone stop", IdempotencyKey: "p-1", Data: map[string]any{"why": "a storm"}})
	if err != nil || !stop.Created || len(stop.Deliveries) != 2 {
		t.Fatalf("publish = %+v %v", stop, err)
	}
	if again, err := local.PublishGroupEvent(ctx, application.PublishGroupEventCommand{Group: "ops", Name: "stop.all", Message: "Everyone stop", IdempotencyKey: "p-1", Data: map[string]any{"why": "a storm"}}); err != nil || again.Created || again.EventID != stop.EventID {
		t.Errorf("again = %+v %v", again, err)
	}
	if _, err := local.PublishGroupEvent(ctx, application.PublishGroupEventCommand{Group: "ops", Name: "error.bot", Message: "x", IdempotencyKey: "p-2"}); err == nil || !strings.Contains(err.Error(), domain.RefusedNotAllowed) {
		t.Errorf("error.bot: %v", err)
	}
	topics, err := local.ListGroupTopics(ctx, "ops", "ops")
	names := []string{}
	for _, topic := range topics {
		names = append(names, topic.Name)
	}
	if err != nil || strings.Join(names, ",") != "error.*,stop.all,warn.deploy-slow" {
		t.Errorf("topics = %v %v", names, err)
	}
}

func TestStopAndUrgentOverTheSocket(t *testing.T) {
	_, local, _ := serve(t, "completed")
	ctx := context.Background()
	if _, err := local.RegisterBot(ctx, definition("worker-1", "observe")); err != nil {
		t.Fatal(err)
	}
	if _, err := local.EnableBot(ctx, "worker-1"); err != nil {
		t.Fatal(err)
	}
	first, err := local.SubmitTask(ctx, application.SubmitRequest{BotID: "worker-1", Objective: "Watch", IdempotencyKey: "k-1"})
	if err != nil {
		t.Fatal(err)
	}
	urgent, err := local.SubmitTask(ctx, application.SubmitRequest{BotID: "worker-1", Objective: "Roll back", IdempotencyKey: "k-2", Priority: "urgent"})
	if err != nil || urgent.Task.Priority != "urgent" {
		t.Fatalf("urgent = %+v %v", urgent.Task, err)
	}
	// The urgent task drops the queued one: only the urgent one is left to stop.
	if task, _ := local.GetTask(ctx, first.Task.ID); task.Status != "Cancelled" || !strings.HasPrefix(task.FailureReason, "dropped from the queue by the urgent task "+urgent.Task.ID) {
		t.Errorf("first = %+v", task)
	}
	stopped, err := local.StopBot(ctx, "worker-1")
	if err != nil || stopped.Bot.Status != "Paused" || len(stopped.Cancelled) != 1 || stopped.Cancelled[0] != urgent.Task.ID || len(stopped.NotCancelled) != 0 {
		t.Fatalf("stop = %+v %v", stopped, err)
	}
	if task, _ := local.GetTask(ctx, urgent.Task.ID); task.Status != "Cancelled" || !strings.HasPrefix(task.FailureReason, "stopped by ") {
		t.Errorf("urgent = %+v", task)
	}
	if _, err := local.StopBot(ctx, "ghost"); !domain.IsNotFound(err) {
		t.Errorf("ghost: %v", err)
	}
	if _, err := local.StopBot(ctx, "Bad Id"); err == nil {
		t.Error("a bad id was stopped")
	}
}
