package application

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

// loudRecepcion lets recepcion emit up to 60 events a minute.
func loudRecepcion(t *testing.T, h *harness) {
	t.Helper()
	if _, err := NewUpdateBot(h.deps).Execute(context.Background(), "recepcion", BotPatch{Events: &domain.EventsSnapshot{PerMinute: 60}}); err != nil {
		t.Fatal(err)
	}
}

func TestAReceiversIntakeTurnsTasksIntoNotes(t *testing.T) {
	h := newHarness()
	parent := shopTeam(t, h)
	loudRecepcion(t, h)
	subscribe(t, h, "almacen", domain.SubscriptionSnapshot{Topic: "pedido.*", Mode: "task"}, domain.SubscriptionSnapshot{Topic: "info.*", Mode: "note"})
	// Notes do not count.
	for i := 1; i <= 11; i++ {
		emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: fmt.Sprintf("info.n%d", i), Message: "x"})
	}
	for i := 1; i <= 10; i++ {
		answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: fmt.Sprintf("pedido.n%d", i), Message: "an order"})
		if len(answer.Deliveries) != 1 || answer.Deliveries[0].Mode != "task" {
			t.Fatalf("order %d = %+v", i, answer)
		}
	}
	eleventh := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.n11", Message: "an order"})
	if d := eleventh.Deliveries; len(d) != 1 || d[0].Mode != "note" || d[0].Degraded != DegradedIntakeRate || d[0].TaskID != "" {
		t.Errorf("11th = %+v", eleventh)
	}
	if heard, _ := lastEvent(h, domain.EventEventHeard); heard.Payload["degraded"] != DegradedIntakeRate || heard.Payload["mode"] != "note" {
		t.Errorf("heard = %+v", heard)
	}
	stop := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "stop.halt", Message: "stop", To: []string{"almacen"}})
	if d := stop.Deliveries; len(d) != 1 || d[0].Mode != "task" || !d[0].Urgent {
		t.Errorf("12th, a stop = %+v", stop)
	}
}

func TestAMessageOverTheIntakeIsANote(t *testing.T) {
	h := newHarness()
	parent := shopTeam(t, h)
	loudRecepcion(t, h)
	strict := 1
	if _, err := NewUpdateBot(h.deps).Execute(context.Background(), "supervisor", BotPatch{Events: &domain.EventsSnapshot{IntakePerMinute: strict}}); err != nil {
		t.Fatal(err)
	}
	subscribe(t, h, "supervisor", domain.SubscriptionSnapshot{Topic: "warn.*"})
	submitWith(t, h, "supervisor", "watch", "")
	nextFor(t, h, "web-1")
	first := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "warn.a", Message: "x"})
	second := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "warn.b", Message: "x"})
	if first.Deliveries[0].Mode != "message" || second.Deliveries[0].Mode != "note" || second.Deliveries[0].Degraded != DegradedIntakeRate {
		t.Errorf("first %+v second %+v", first, second)
	}
}

func TestEventsOfOneNameCoalesceIntoAPendingTask(t *testing.T) {
	h := newHarness()
	parent := shopTeam(t, h)
	loudRecepcion(t, h)
	subscribe(t, h, "almacen", domain.SubscriptionSnapshot{Topic: "pedido.*", Mode: "task"})
	first := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "table 4"})
	second := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "table 7"})
	if first.Deliveries[0].Mode != "task" || second.Deliveries[0].Mode != "note" || second.Deliveries[0].CoalescedInto != first.Deliveries[0].TaskID {
		t.Fatalf("first %+v second %+v", first, second)
	}
	if heard, _ := lastEvent(h, domain.EventEventHeard); heard.Payload["coalescedInto"] != first.Deliveries[0].TaskID {
		t.Errorf("heard = %+v", heard)
	}
	other := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.cancelado", Message: "table 4"})
	if other.Deliveries[0].Mode != "task" || other.Deliveries[0].TaskID == first.Deliveries[0].TaskID {
		t.Errorf("another name = %+v", other)
	}
	report(t, h, parent)
	started := nextFor(t, h, "web-1")
	if started.Run.TaskID.String() != first.Deliveries[0].TaskID || !strings.Contains(started.Run.Prompt.String(), "recepcion (pedido.nuevo): table 7") {
		t.Errorf("started = %s\n%s", started.Run.TaskID, started.Run.Prompt)
	}
}

func TestCriticalsAreNeverCoalesced(t *testing.T) {
	h := newHarness()
	parent := shopTeam(t, h)
	first := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "stop.halt", Message: "one", To: []string{"almacen"}})
	second := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "stop.halt", Message: "two", To: []string{"almacen"}})
	if !first.Deliveries[0].Urgent || !second.Deliveries[0].Urgent || second.Deliveries[0].Mode != "task" || second.Deliveries[0].TaskID == first.Deliveries[0].TaskID {
		t.Errorf("first %+v second %+v", first, second)
	}
}

// chainTeam is four bots in ops, each listening to warn.* as tasks.
func chainTeam(t *testing.T, h *harness, mutate func(*BotDefinition)) Assignment {
	t.Helper()
	onlineNode(t, h, "web-1")
	var a domain.Bot
	for _, id := range []string{"a", "b", "c", "d"} {
		bot := teamBot(t, h, id, []string{"ops"}, func(d *BotDefinition) {
			d.Subscriptions = []domain.SubscriptionSnapshot{{Topic: "warn." + map[string]string{"a": "z", "b": "a", "c": "b", "d": "c"}[id], Mode: "task"}}
			if id == "a" && mutate != nil {
				mutate(d)
			}
		})
		if id == "a" {
			a = bot
		}
	}
	return runOnNode(t, h, a, "web-1", "ask-1")
}

// hop reports the assignment's execution, runs the next task and emits
// warn.<from> from it.
func hop(t *testing.T, h *harness, from string, run Assignment) (EmitAnswer, Assignment) {
	t.Helper()
	answer := emit(t, h, "web-1", run.ExecutionID, EmitEventCommand{Name: "warn." + from, Message: "on to you"})
	report(t, h, run)
	if len(answer.Deliveries) == 0 {
		return answer, Assignment{}
	}
	return answer, nextFor(t, h, "web-1")
}

func TestAnEventsChainGoesTwoDeepByDefault(t *testing.T) {
	h := newHarness()
	run := chainTeam(t, h, nil)
	first, run := hop(t, h, "a", run)
	if len(first.Deliveries) != 1 || first.Deliveries[0].BotID != "b" {
		t.Fatalf("a → b = %+v", first)
	}
	second, run := hop(t, h, "b", run)
	if len(second.Deliveries) != 1 || second.Deliveries[0].BotID != "c" {
		t.Fatalf("b → c = %+v", second)
	}
	third, _ := hop(t, h, "c", run)
	if len(third.Skipped) != 1 || third.Skipped[0].BotID != "d" || third.Skipped[0].Reason != domain.RefusedDepthExceeded {
		t.Errorf("c → d = %+v", third)
	}
}

func TestAnEmittersMaxDepthCutsItsChain(t *testing.T) {
	h := newHarness()
	run := chainTeam(t, h, func(d *BotDefinition) { d.Events = &domain.EventsSnapshot{MaxDepth: 1} })
	if _, err := domain.NewEventPolicy(domain.EventsSnapshot{MaxDepth: 9}); err == nil {
		t.Error("maxDepth 9 was accepted")
	}
	first, run := hop(t, h, "a", run)
	if len(first.Deliveries) != 1 {
		t.Fatalf("a → b = %+v", first)
	}
	second, _ := hop(t, h, "b", run)
	if len(second.Skipped) != 1 || second.Skipped[0].Reason != domain.RefusedDepthExceeded {
		t.Errorf("b → c = %+v", second)
	}
}
