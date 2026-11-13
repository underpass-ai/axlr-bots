package application

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// deletable builds worker-1 with a task running on a node, a delegated
// child on its teammate worker-2, a pending task, an armed timer and a notification to send; worker-2
// listens to it and watcher may delegate to it.
func deletable(t *testing.T) (*harness, Assignment, domain.Task) {
	t.Helper()
	h := newHarness()
	ctx := context.Background()
	onlineNode(t, h, "web-1")
	worker := teamBot(t, h, "worker-1", []string{"ops"}, func(d *BotDefinition) {
		d.Triggers = []domain.TriggerSnapshot{{Type: "manual"}, {Type: "timer", Name: "clock", Every: "10m", Objective: "Look around"}}
	})
	teamBot(t, h, "worker-2", []string{"ops"}, func(d *BotDefinition) {
		d.Subscriptions = []domain.SubscriptionSnapshot{{Topic: "warn.*", From: []string{"worker-1"}}}
	})
	delegationBot(t, h, "watcher", &domain.DelegationSnapshot{To: []string{"worker-1"}}, BudgetsDefinition{MaxModelCallsPerTask: 10, MaxToolCallsPerTask: 10, TaskTimeoutSeconds: 300})
	running := runOnNode(t, h, worker, "web-1", "k-run")
	if answer := delegate(t, h, "web-1", running.ExecutionID, "worker-2", "Look at the disk"); answer.TaskID == "" {
		t.Fatalf("no child: %+v", answer)
	}
	pending := submit(t, h, worker, "k-pending").Task
	if _, err := NewTickTimers(h.deps).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	target, _ := domain.NewNotifyTarget(domain.NotifyTargetSnapshot{Type: "command", Name: "ops", Command: []string{"/bin/true"}})
	delivery, _ := domain.NewDelivery("dlv-1", "worker-1", pending.ID(), "exec-x", "c1", target, []byte(`{}`), t0)
	if err := h.outbox.Save(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	h.log.events, h.log.audits = nil, nil
	return h, running, pending
}

func TestDeleteBotEndsWhatItLeftGoingAndKeepsItsHistory(t *testing.T) {
	h, running, _ := deletable(t)
	ctx := context.Background()
	result, err := NewDeleteBot(h.deps).Execute(ctx, "worker-1")
	if err != nil {
		t.Fatal(err)
	}
	// The running task, the pending one and the timer's.
	if len(result.Cancelled) != 3 || len(result.ChildrenCancelled) != 1 || result.TimersRemoved != 1 || result.NotificationsDropped != 1 {
		t.Fatalf("result = %+v", result)
	}
	want := []BotReference{{BotID: "watcher", How: ReferenceDelegation}, {BotID: "worker-2", How: ReferenceSubscription, Topic: "warn.*"}}
	if !slices.Equal(result.NamedBy, want) {
		t.Errorf("namedBy = %+v", result.NamedBy)
	}
	if _, err := h.bots.Get(ctx, "worker-1"); !domain.IsNotFound(err) {
		t.Errorf("bot still there: %v", err)
	}
	for _, id := range append(result.Cancelled, result.ChildrenCancelled...) {
		task := taskOf(t, h, id)
		if task.Status() != domain.TaskCancelled {
			t.Errorf("task %s is %s", id, task.Status())
		}
	}
	if task := taskOf(t, h, result.Cancelled[0]); task.FailureReason() != DeletedReason {
		t.Errorf("reason = %q", task.FailureReason())
	}
	execution, _ := h.execs.Get(ctx, running.ExecutionID)
	if execution.Outcome() != domain.ExecutionCancelled {
		t.Errorf("execution = %s", execution.Outcome())
	}
	if timers, _ := h.timers.List(ctx); len(timers) != 0 {
		t.Errorf("timers = %d", len(timers))
	}
	if pending, _ := h.outbox.ListPending(ctx); len(pending) != 0 {
		t.Errorf("deliveries = %d", len(pending))
	}
	if deleted, ok := lastEvent(h, domain.EventBotDeleted); !ok || deleted.BotID != "worker-1" || deleted.Payload["cancelled"] != 3 {
		t.Errorf("bot.deleted = %+v", deleted)
	}
	if last := h.log.audits[len(h.log.audits)-1]; last.Action != "bot.delete" || last.Outcome != domain.AuditOK {
		t.Errorf("audit = %+v", last)
	}
	// History stays: the tasks are there; a second delete finds no bot.
	if all, _ := h.tasks.List(ctx); len(all) != 4 {
		t.Errorf("tasks = %d", len(all))
	}
	if _, err := NewDeleteBot(h.deps).Execute(ctx, "worker-1"); !domain.IsNotFound(err) {
		t.Errorf("second delete: %v", err)
	}
}

func TestABotRegisteredAgainDoesNotFindItsPredecessorsTask(t *testing.T) {
	h, _, _ := deletable(t)
	ctx := context.Background()
	old, _, _ := h.tasks.FindByIdempotencyKey(ctx, "worker-1", "k-pending", time.Time{})
	if _, err := NewDeleteBot(h.deps).Execute(ctx, "worker-1"); err != nil {
		t.Fatal(err)
	}
	h.clock.now = t0.Add(time.Minute)
	again := teamBot(t, h, "worker-1", []string{"ops"}, nil)
	fresh := submit(t, h, again, "k-pending")
	if !fresh.Created || fresh.Task.ID() == old.ID() || fresh.Task.Status() != domain.TaskPending {
		t.Fatalf("resubmit found the old task: %+v", fresh.Task.Snapshot())
	}
	if repeat := submit(t, h, again, "k-pending"); repeat.Created || repeat.Task.ID() != fresh.Task.ID() {
		t.Errorf("the new key is not idempotent: %+v", repeat.Task.Snapshot())
	}
}

func TestDeleteBotStopsBeforeTheRecordWhenAStoreFails(t *testing.T) {
	ctx := context.Background()
	for name, breakIt := range map[string]func(*harness){
		"tasks":  func(h *harness) { h.tasks.listFail = errBoom },
		"timers": func(h *harness) { h.timers.fail = errBoom },
		"save":   func(h *harness) { h.tasks.fail = errBoom },
	} {
		h, _, _ := deletable(t)
		breakIt(h)
		if _, err := NewDeleteBot(h.deps).Execute(ctx, "worker-1"); !errors.Is(err, errBoom) {
			t.Errorf("%s: %v", name, err)
		}
		if _, err := h.bots.Get(ctx, "worker-1"); err != nil {
			t.Errorf("%s: the bot went anyway: %v", name, err)
		}
	}
	h := newHarness()
	if _, err := NewDeleteBot(h.deps).Execute(ctx, "ghost"); !domain.IsNotFound(err) {
		t.Errorf("ghost: %v", err)
	}
}

// A deleted bot's history stays readable through its events, bot.deleted
// last (it answered not found once the record was gone); an id that was
// never a bot is still not found.
func TestListEventsOfADeletedBotKeepsItsHistory(t *testing.T) {
	h, _, _ := deletable(t)
	ctx := context.Background()
	if _, err := NewDeleteBot(h.deps).Execute(ctx, "worker-1"); err != nil {
		t.Fatal(err)
	}
	events, err := NewListEvents(h.deps).Execute(ctx, "worker-1", 0)
	if err != nil || len(events) == 0 || events[len(events)-1].Type != domain.EventBotDeleted {
		t.Fatalf("events = %v %v", events, err)
	}
	if _, err := NewListEvents(h.deps).Execute(ctx, "never-there", 0); !domain.IsNotFound(err) {
		t.Errorf("an id with no bot and no events: %v", err)
	}
}
