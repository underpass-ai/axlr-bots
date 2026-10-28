package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

func ask(t *testing.T, h *harness, a Assignment, topic, why string) SubscribeAnswer {
	t.Helper()
	answer, err := NewAskSubscription(h.deps).Execute(context.Background(), "web-1", a.ExecutionID, SubscribeCommand{Topic: topic, Why: why})
	if err != nil {
		t.Fatal(err)
	}
	return answer
}

func TestABotAsksAndThePersonDecides(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := shopTeam(t, h)
	// almacen runs a task and asks to hear errors.
	submitWith(t, h, "almacen", "stock", "")
	running := nextFor(t, h, "web-1")
	answer := ask(t, h, running, "error.*", "para reintentar si recepcion falla")
	if answer.Refused != nil || answer.Status != "pending" || answer.Topic != "error.*" {
		t.Fatalf("answer = %+v", answer)
	}
	bot, _ := h.bots.Get(ctx, "almacen")
	got := bot.Snapshot().Subscriptions
	if len(got) != 1 || got[0].Status != "pending" || got[0].AskedBy != "almacen" || got[0].Why != "para reintentar si recepcion falla" || got[0].AskedAt == nil {
		t.Fatalf("subscriptions = %+v", got)
	}
	if asked, ok := lastEvent(h, domain.EventSubscriptionAsked); !ok || asked.BotID != "almacen" || asked.Payload["topic"] != "error.*" {
		t.Errorf("asked = %+v", asked)
	}
	// A pending subscription hears nothing: recepcion's failure reaches
	// nobody.
	fail(t, h, parent, "boom")
	if emitted := emittedBy(h, "recepcion"); len(emitted) != 0 {
		t.Errorf("a pending ask heard %+v", emitted)
	}
	if again := ask(t, h, running, "error.*", "again"); again.Refused == nil || again.Refused.Reason != domain.RefusedAlreadySubscribed {
		t.Errorf("again = %+v", again)
	}
	// The person approves: the same entry, active.
	approved := got[0]
	approved.Status = "active"
	subscribe(t, h, "almacen", approved)
	if changed, _ := lastEvent(h, domain.EventSubscriptionChanged); changed.BotID != "almacen" {
		t.Errorf("changed = %+v", changed)
	}
	second := runOnNode(t, h, mustBot(t, h, "recepcion"), "web-1", "ask-2")
	report(t, h, running)
	fail(t, h, second, "boom again")
	if emitted := emittedBy(h, "recepcion"); len(emitted) != 1 || len(emitted[0].Payload["deliveries"].([]map[string]any)) != 1 {
		t.Errorf("after approval = %+v", emitted)
	}
}

func mustBot(t *testing.T, h *harness, id string) domain.Bot {
	t.Helper()
	bot, err := h.bots.Get(context.Background(), domain.BotID(id))
	if err != nil {
		t.Fatal(err)
	}
	return bot
}

func TestAnAskIsRefusedWhenItCannotBeTaken(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := shopTeam(t, h)
	many := make([]domain.SubscriptionSnapshot, domain.MaxSubscriptions)
	for i := range many {
		many[i].Topic = "t" + string(rune('a'+i))
	}
	subscribe(t, h, "recepcion", many...)
	if full := ask(t, h, parent, "error.*", "x"); full.Refused == nil || full.Refused.Reason != domain.RefusedTooMany {
		t.Errorf("full = %+v", full)
	}
	if mine := ask(t, h, parent, "ta", "x"); mine.Refused == nil || mine.Refused.Reason != domain.RefusedAlreadySubscribed {
		t.Errorf("mine = %+v", mine)
	}
	if _, err := NewAskSubscription(h.deps).Execute(ctx, "web-1", parent.ExecutionID, SubscribeCommand{Topic: "Bad", Why: "x"}); err == nil {
		t.Error("a bad topic was taken")
	}
	if _, err := NewAskSubscription(h.deps).Execute(ctx, "web-1", parent.ExecutionID, SubscribeCommand{Topic: "x", Why: " "}); err == nil {
		t.Error("an ask without why was taken")
	}
	if _, err := NewAskSubscription(h.deps).Execute(ctx, "web-2", parent.ExecutionID, SubscribeCommand{Topic: "x", Why: "y"}); err == nil {
		t.Error("another node's execution asked")
	}
	alone := teamBot(t, h, "alone", nil, nil)
	report(t, h, parent)
	lonely := runOnNode(t, h, alone, "web-1", "ask-9")
	if answer := ask(t, h, lonely, "x", "y"); answer.Refused == nil || answer.Refused.Reason != domain.RefusedNoTeam {
		t.Errorf("no team = %+v", answer)
	}
}

func publish(t *testing.T, h *harness, command PublishGroupEventCommand) PublishAnswer {
	t.Helper()
	answer, err := NewPublishGroupEvent(h.deps).Execute(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	return answer
}

func TestTheOperatorPublishesOnATeam(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	shopTeam(t, h)
	subscribe(t, h, "almacen", domain.SubscriptionSnapshot{Topic: "pedido.*", Mode: "task"})
	stop := publish(t, h, PublishGroupEventCommand{Group: "tienda", Name: "stop.all", Message: "Everyone stop", IdempotencyKey: "k-1"})
	if !stop.Created || len(stop.Deliveries) != 4 {
		t.Fatalf("stop.all = %+v", stop)
	}
	for _, d := range stop.Deliveries {
		task := taskOf(t, h, domain.TaskID(d.TaskID))
		if !d.Urgent || d.Mode != "task" || !strings.HasPrefix(task.Objective().String(), "Your operator says (stop.all): Everyone stop") {
			t.Errorf("delivery = %+v %q", d, task.Objective())
		}
	}
	events, err := NewListGroupEvents(h.deps).Execute(ctx, GroupEventsQuery{Name: "tienda", Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	published := events[len(events)-1]
	if published.Type != domain.EventGroupEventPublished || published.GroupName != "tienda" || published.BotID != "" || len(published.Payload["deliveries"].([]map[string]any)) != 4 {
		t.Errorf("published = %+v", published)
	}
	before := len(h.log.events)
	again := publish(t, h, PublishGroupEventCommand{Group: "tienda", Name: "stop.all", Message: "Everyone stop", IdempotencyKey: "k-1"})
	if again.Created || again.EventID != stop.EventID || len(again.Deliveries) != 4 || again.Deliveries[0].Created || len(h.log.events) != before {
		t.Errorf("again = %+v (%d new events)", again, len(h.log.events)-before)
	}
	// An addressed info is a note; a message reaches a running task.
	note := publish(t, h, PublishGroupEventCommand{Group: "tienda", Name: "info.precios", Message: "new prices", To: []string{"caja", "caja"}, IdempotencyKey: "k-2", Data: map[string]any{"from": "monday"}})
	if len(note.Deliveries) != 1 || note.Deliveries[0].Mode != "note" {
		t.Errorf("note = %+v", note)
	}
	for name, command := range map[string]PublishGroupEventCommand{
		"error":    {Group: "tienda", Name: "error.bot", Message: "x", IdempotencyKey: "k"},
		"done":     {Group: "tienda", Name: "done.task", Message: "x", IdempotencyKey: "k"},
		"stranger": {Group: "tienda", Name: "info.x", Message: "x", To: []string{"stranger"}, IdempotencyKey: "k"},
		"no key":   {Group: "tienda", Name: "info.x", Message: "x"},
		"bad name": {Group: "tienda", Name: "Bad", Message: "x", IdempotencyKey: "k"},
		"message":  {Group: "tienda", Name: "info.x", IdempotencyKey: "k"},
		"data":     {Group: "tienda", Name: "info.x", Message: "x", Data: map[string]any{"big": strings.Repeat("x", MaxEventData)}, IdempotencyKey: "k"},
		"too many": {Group: "tienda", Name: "info.x", Message: "x", To: make([]string, 17), IdempotencyKey: "k"},
		"bad to":   {Group: "tienda", Name: "info.x", Message: "x", To: []string{"Bad Id"}, IdempotencyKey: "k"},
	} {
		_, err := NewPublishGroupEvent(h.deps).Execute(ctx, command)
		var invalid domain.ValidationError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: %v", name, err)
		}
		if name == "error" && !strings.Contains(err.Error(), domain.RefusedNotAllowed) {
			t.Errorf("error.bot: %v", err)
		}
	}
	if _, err := NewPublishGroupEvent(h.deps).Execute(ctx, PublishGroupEventCommand{Group: "missing", Name: "info.x", Message: "x", IdempotencyKey: "k"}); err == nil {
		t.Error("a missing team was published on")
	}
}

func TestATeamsTopics(t *testing.T) {
	h := newHarness()
	parent := shopTeam(t, h)
	subscribe(t, h, "almacen", domain.SubscriptionSnapshot{Topic: "pedido.*", Mode: "task"}, domain.SubscriptionSnapshot{Topic: "error.*", Status: ""})
	if _, err := NewUpdateBot(h.deps).Execute(context.Background(), "caja", BotPatch{Events: &domain.EventsSnapshot{Emit: []domain.EmitRuleSnapshot{{Name: "warn.caja"}}}}); err != nil {
		t.Fatal(err)
	}
	emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "table 4"})
	emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "info.turno", Message: "shift"})
	emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "info.dicho", Message: "to you", To: []string{"caja"}})
	publish(t, h, PublishGroupEventCommand{Group: "tienda", Name: "stop.all", Message: "Everyone stop", IdempotencyKey: "k-1"})
	topics, err := NewListGroupTopics(h.deps).Execute(context.Background(), "tienda", "")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]GroupTopic{}
	for _, topic := range topics {
		byName[topic.Name] = topic
	}
	pedido := byName["pedido.nuevo"]
	if strings.Join(pedido.Publishers, ",") != "recepcion" || len(pedido.Listeners) != 1 || pedido.Listeners[0].BotID != "almacen" || pedido.Listeners[0].Mode != "task" || pedido.Count != 1 || pedido.LastAt == nil || pedido.NobodyListens {
		t.Errorf("pedido.nuevo = %+v", pedido)
	}
	if stop := byName["stop.all"]; strings.Join(stop.Publishers, ",") != "operator" || stop.NobodyListens {
		t.Errorf("stop.all = %+v", stop)
	}
	if turno := byName["info.turno"]; !turno.NobodyListens {
		t.Errorf("info.turno = %+v", turno)
	}
	if dicho := byName["info.dicho"]; dicho.NobodyListens {
		t.Errorf("an addressed event = %+v", dicho)
	}
	if pattern := byName["pedido.*"]; len(pattern.Listeners) != 1 || pattern.Count != 0 || pattern.LastAt != nil {
		t.Errorf("pedido.* = %+v", pattern)
	}
	if declared := byName["warn.caja"]; strings.Join(declared.Declared, ",") != "caja" {
		t.Errorf("warn.caja = %+v", declared)
	}
	if names := len(topics); names != 7 || topics[0].Name > topics[1].Name {
		t.Errorf("topics = %+v", topics)
	}
}
