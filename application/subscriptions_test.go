package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// shopTeam is the ADR's guide case: recepcion, almacen, supervisor and
// caja in tienda, and a stranger elsewhere; recepcion runs a task.
func shopTeam(t *testing.T, h *harness) Assignment {
	t.Helper()
	onlineNode(t, h, "web-1")
	recepcion := teamBot(t, h, "recepcion", []string{"tienda"}, nil)
	teamBot(t, h, "almacen", []string{"tienda"}, nil)
	teamBot(t, h, "supervisor", []string{"tienda"}, nil)
	teamBot(t, h, "caja", []string{"tienda"}, nil)
	teamBot(t, h, "stranger", []string{"other"}, nil)
	return runOnNode(t, h, recepcion, "web-1", "ask-1")
}

func subscribe(t *testing.T, h *harness, bot string, subs ...domain.SubscriptionSnapshot) domain.Bot {
	t.Helper()
	updated, err := NewUpdateBot(h.deps).Execute(context.Background(), domain.BotID(bot), BotPatch{Subscriptions: &subs})
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

func TestASubscriberHearsItsTopicAsATask(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := shopTeam(t, h)
	almacen := subscribe(t, h, "almacen", domain.SubscriptionSnapshot{Topic: "pedido.*", Mode: "task", From: []string{"recepcion"}})
	if changed, ok := lastEvent(h, domain.EventSubscriptionChanged); !ok || changed.BotID != "almacen" || changed.Payload["by"] != "operator" || len(changed.Payload["subscriptions"].([]domain.SubscriptionSnapshot)) != 1 {
		t.Errorf("changed = %+v", changed)
	}
	if got := almacen.Snapshot().Subscriptions; len(got) != 1 || got[0].Topic != "pedido.*" {
		t.Errorf("subscriptions = %+v", got)
	}
	answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "two boxes for table 4"})
	if answer.Refused != nil || len(answer.Deliveries) != 1 || len(answer.Skipped) != 0 {
		t.Fatalf("answer = %+v", answer)
	}
	d := answer.Deliveries[0]
	if d.BotID != "almacen" || d.Mode != "task" || d.Trigger != "pedido.*" || d.Urgent {
		t.Errorf("delivery = %+v", d)
	}
	task, err := h.tasks.Get(ctx, domain.TaskID(d.TaskID))
	if err != nil {
		t.Fatal(err)
	}
	if ref := task.Trigger(); ref.Type != domain.TriggerEvent || ref.Name != "pedido.*" || task.IsUrgent() || !strings.HasPrefix(task.Objective().String(), "Your teammate recepcion says (pedido.nuevo): two boxes for table 4") {
		t.Errorf("task = %+v %q", ref, task.Objective())
	}
	heard, ok := lastEvent(h, domain.EventEventHeard)
	if !ok || heard.BotID != "almacen" || heard.Payload["mode"] != "task" || heard.Payload["from"] != "recepcion" || heard.Payload["eventId"] != answer.EventID || heard.Payload["taskId"] != d.TaskID {
		t.Errorf("heard = %+v", heard)
	}
	emitted, _ := lastEvent(h, domain.EventEventEmitted)
	if deliveries := emitted.Payload["deliveries"].([]map[string]any); deliveries[0]["mode"] != "task" {
		t.Errorf("emitted = %+v", emitted.Payload)
	}
	// The same event again is the same delivery; nothing is heard twice.
	before := len(h.log.events)
	if again := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "two boxes for table 4"}); !again.Deduplicated || again.Deliveries[0].Created {
		t.Errorf("again = %+v", again)
	}
	for _, e := range h.log.events[before:] {
		if e.Type == domain.EventEventHeard {
			t.Error("a repeat was heard twice")
		}
	}
}

func TestAnEventFromOutsideFromIsNotHeard(t *testing.T) {
	h := newHarness()
	onlineNode(t, h, "web-1")
	teamBot(t, h, "recepcion", []string{"tienda"}, nil)
	caja := teamBot(t, h, "caja", []string{"tienda"}, nil)
	teamBot(t, h, "almacen", []string{"tienda"}, nil)
	subscribe(t, h, "almacen", domain.SubscriptionSnapshot{Topic: "pedido.*", From: []string{"recepcion"}})
	run := runOnNode(t, h, caja, "web-1", "ask-1")
	if answer := emit(t, h, "web-1", run.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "x"}); len(answer.Deliveries) != 0 || len(answer.Skipped) != 0 {
		t.Errorf("answer = %+v", answer)
	}
}

func TestTheTeamsDefaultAndAddressedEvents(t *testing.T) {
	h := newHarness()
	parent := shopTeam(t, h)
	steady := false
	if _, err := NewUpdateBot(h.deps).Execute(context.Background(), "caja", BotPatch{Events: &domain.EventsSnapshot{Interruptible: &steady}}); err != nil {
		t.Fatal(err)
	}
	subscribe(t, h, "supervisor", domain.SubscriptionSnapshot{Topic: "pedido.*"})
	fire := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "danger.fire", Message: "smoke in the store"})
	urgent := map[string]bool{}
	for _, d := range fire.Deliveries {
		urgent[d.BotID] = d.Urgent
	}
	// Supervisor subscribes to something else: the team's default is for
	// bots that subscribe to nothing.
	if len(fire.Deliveries) != 2 || !urgent["almacen"] || urgent["caja"] {
		t.Errorf("danger.fire = %+v", fire)
	}
	recall := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "stop.recall", Message: "back", To: []string{"supervisor"}})
	if len(recall.Deliveries) != 1 || !recall.Deliveries[0].Urgent || recall.Deliveries[0].Trigger != "team" {
		t.Errorf("stop.recall = %+v", recall)
	}
	alias := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "recall", Message: "back now", To: []string{"almacen"}})
	if alias.Name != "stop.recall" || len(alias.Deliveries) != 1 || !alias.Deliveries[0].Urgent {
		t.Errorf("recall = %+v", alias)
	}
	warn := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "warn.x", Message: "low stock", To: []string{"almacen"}})
	if len(warn.Deliveries) != 1 || warn.Deliveries[0].Urgent {
		t.Errorf("warn.x = %+v", warn)
	}
	info := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "info.turno", Message: "shift"})
	if len(info.Deliveries) != 0 || len(info.Skipped) != 0 {
		t.Errorf("info.turno = %+v", info)
	}
}

func TestSubscriptionsArePatchedWithinTheirRules(t *testing.T) {
	h := newHarness()
	shopTeam(t, h)
	update := NewUpdateBot(h.deps)
	ctx := context.Background()
	patch := func(subs ...domain.SubscriptionSnapshot) error {
		_, err := update.Execute(ctx, "almacen", BotPatch{Subscriptions: &subs})
		return err
	}
	many := make([]domain.SubscriptionSnapshot, 17)
	for i := range many {
		many[i].Topic = "t" + string(rune('a'+i))
	}
	for name, err := range map[string]error{
		"17":       patch(many...),
		"pattern":  patch(domain.SubscriptionSnapshot{Topic: "Pedido"}),
		"stranger": patch(domain.SubscriptionSnapshot{Topic: "pedido.*", From: []string{"stranger"}}),
		"pending":  patch(domain.SubscriptionSnapshot{Topic: "error.*", Status: "pending"}),
	} {
		var invalid domain.ValidationError
		if !errors.As(err, &invalid) || !strings.HasPrefix(invalid.Field, "subscription") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := patch(domain.SubscriptionSnapshot{Topic: "pedido.*", From: []string{"recepcion", "operator", "control-plane"}}); err != nil {
		t.Errorf("teammates, operator, control-plane: %v", err)
	}
	// A bot's ask may be kept as it is, made active or dropped; not edited.
	asked := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	ask := domain.SubscriptionSnapshot{Topic: "error.*", Status: "pending", AskedBy: "almacen", Why: "para reintentar", AskedAt: &asked}
	bot, _ := h.bots.Get(ctx, "almacen")
	subs, _ := domain.NewSubscriptions([]domain.SubscriptionSnapshot{ask})
	bot.SetSubscriptions(subs)
	if err := h.bots.Save(ctx, bot); err != nil {
		t.Fatal(err)
	}
	if err := patch(ask, domain.SubscriptionSnapshot{Topic: "pedido.*"}); err != nil {
		t.Errorf("kept: %v", err)
	}
	edited := ask
	edited.Mode = "note"
	if err := patch(edited); err == nil {
		t.Error("an edited ask was accepted")
	}
	approved := ask
	approved.Status = "active"
	if err := patch(approved); err != nil {
		t.Errorf("approved: %v", err)
	}
	if got, _ := h.bots.Get(ctx, "almacen"); !got.Subscriptions().List()[0].Active() {
		t.Error("the ask is not active")
	}
}

func TestAnEventTriggerIsReadAsASubscription(t *testing.T) {
	h := newHarness()
	bot := teamBot(t, h, "old", []string{"port"}, func(d *BotDefinition) {
		d.Triggers = []domain.TriggerSnapshot{{Type: "manual"}, {Type: "event", Events: []string{"a.*"}, Interrupt: true}}
	})
	got := bot.Snapshot()
	if len(got.Subscriptions) != 1 || got.Subscriptions[0].Topic != "a.*" || got.Subscriptions[0].Mode != "task" || !got.Subscriptions[0].Urgent || len(got.Triggers) != 1 {
		t.Errorf("snapshot = %+v %+v", got.Subscriptions, got.Triggers)
	}
	patched, err := NewUpdateBot(h.deps).Execute(context.Background(), "old", BotPatch{Triggers: &[]domain.TriggerSnapshot{{Type: "event", Events: []string{"b"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if subs := patched.Snapshot().Subscriptions; len(subs) != 2 || subs[1].Topic != "b" {
		t.Errorf("patched = %+v", subs)
	}
}
