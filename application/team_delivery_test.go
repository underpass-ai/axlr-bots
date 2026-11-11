package application

import (
	"context"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

// teamTopic is a team subscription to pedido.* from recepcion.
func teamTopic() domain.SubscriptionSnapshot {
	return domain.SubscriptionSnapshot{Topic: "pedido.*", Mode: "task", From: []string{"recepcion"}, Deliver: "team"}
}

// shopTeamListens is shopTeam with almacen, caja and supervisor on a team
// subscription to pedido.*.
func shopTeamListens(t *testing.T, h *harness) Assignment {
	t.Helper()
	parent := shopTeam(t, h)
	for _, id := range []string{"almacen", "caja", "supervisor"} {
		subscribe(t, h, id, teamTopic())
	}
	return parent
}

func busy(t *testing.T, h *harness, id string, n int) {
	t.Helper()
	bot, err := h.bots.Get(context.Background(), domain.BotID(id))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		submit(t, h, bot, id+"-busy-"+string(rune('a'+i)))
	}
}

func TestATeamEventMakesOneTaskForTheFirstFreeMember(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := shopTeamListens(t, h)
	answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "two boxes"})
	if answer.Refused != nil || len(answer.Deliveries) != 1 || len(answer.Skipped) != 0 {
		t.Fatalf("answer = %+v", answer)
	}
	d := answer.Deliveries[0]
	if d.BotID != "almacen" || d.Mode != "task" || d.Team != "tienda" || !d.Created || d.Trigger != "pedido.*" {
		t.Errorf("delivery = %+v", d)
	}
	task, err := h.tasks.Get(ctx, domain.TaskID(d.TaskID))
	if err != nil {
		t.Fatal(err)
	}
	if task.IdempotencyKey().String() != "event:"+answer.EventID+":team:tienda" {
		t.Errorf("key = %s", task.IdempotencyKey())
	}
	if heard, ok := lastEvent(h, domain.EventEventHeard); !ok || heard.BotID != "almacen" || heard.Payload["team"] != "tienda" {
		t.Errorf("heard = %+v", heard)
	}
	if again := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "two boxes"}); !again.Deduplicated || len(again.Deliveries) != 1 {
		t.Errorf("again = %+v", again)
	}
}

func TestABusyMemberIsPassedOver(t *testing.T) {
	h := newHarness()
	parent := shopTeamListens(t, h)
	busy(t, h, "almacen", 1)
	answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "x"})
	if len(answer.Deliveries) != 1 || answer.Deliveries[0].BotID != "caja" {
		t.Errorf("answer = %+v", answer)
	}
}

func TestWithNobodyFreeTheLeastBusyTakesIt(t *testing.T) {
	h := newHarness()
	parent := shopTeamListens(t, h)
	busy(t, h, "almacen", 2)
	busy(t, h, "caja", 2)
	busy(t, h, "supervisor", 1)
	answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "x"})
	if len(answer.Deliveries) != 1 || answer.Deliveries[0].BotID != "supervisor" {
		t.Errorf("answer = %+v", answer)
	}
}

func TestAnOwnSubscriptionStillHearsBesideTheTeam(t *testing.T) {
	h := newHarness()
	parent := shopTeam(t, h)
	subscribe(t, h, "caja", teamTopic())
	subscribe(t, h, "supervisor", teamTopic())
	subscribe(t, h, "almacen", domain.SubscriptionSnapshot{Topic: "pedido.*", Mode: "task"})
	answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "x"})
	if len(answer.Deliveries) != 2 || answer.Deliveries[0].BotID != "almacen" || answer.Deliveries[0].Team != "" || answer.Deliveries[1].BotID != "caja" || answer.Deliveries[1].Team != "tienda" {
		t.Errorf("answer = %+v", answer)
	}
}

func TestAddressedAndCriticalEventsSkipThePick(t *testing.T) {
	h := newHarness()
	parent := shopTeam(t, h)
	for _, id := range []string{"almacen", "caja", "supervisor"} {
		subscribe(t, h, id, teamTopic(), domain.SubscriptionSnapshot{Topic: "danger.*", Mode: "task", Deliver: "team"})
	}
	addressed := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "x", To: []string{"caja", "supervisor"}})
	if len(addressed.Deliveries) != 2 || addressed.Deliveries[0].Team != "" || addressed.Deliveries[1].Team != "" {
		t.Errorf("addressed = %+v", addressed)
	}
	critical := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "danger.fuego", Message: "fire"})
	if len(critical.Deliveries) != 3 {
		t.Errorf("critical = %+v", critical)
	}
}

func TestAPendingTeamTaskTakesTheNextEventAsANote(t *testing.T) {
	h := newHarness()
	parent := shopTeamListens(t, h)
	first := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "one"})
	second := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "two"})
	if len(second.Deliveries) != 1 {
		t.Fatalf("second = %+v", second)
	}
	d := second.Deliveries[0]
	if d.BotID != "almacen" || d.Mode != "note" || d.CoalescedInto != first.Deliveries[0].TaskID || d.Team != "tienda" {
		t.Errorf("second = %+v", d)
	}
}

func TestATeamEventAfterACrashFindsItsTask(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := shopTeamListens(t, h)
	name, _ := domain.NewEventName("pedido.nuevo")
	message, _ := domain.NewEventMessage("x")
	id := eventID(parent.Run.TaskID, emission{name: name, message: message})
	// The task was made, the event never recorded: caja holds it.
	if _, err := NewSubmitTask(h.deps).Execute(ctx, SubmitTaskCommand{BotID: "caja", Objective: "the event's task", IdempotencyKey: "event:" + id + ":team:tienda"}); err != nil {
		t.Fatal(err)
	}
	answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "x"})
	if answer.EventID != id || len(answer.Deliveries) != 1 || answer.Deliveries[0].BotID != "caja" || answer.Deliveries[0].Created {
		t.Errorf("answer = %+v", answer)
	}
}

func TestATeamDeliveryReachesAMemberOnTheChain(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := shopTeam(t, h)
	subscribe(t, h, "almacen", domain.SubscriptionSnapshot{Topic: "pedido.*", Mode: "task", From: []string{"recepcion"}})
	subscribe(t, h, "recepcion", domain.SubscriptionSnapshot{Topic: "listo.*", Mode: "task", From: []string{"almacen"}, Deliver: "team"})
	first := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "x"})
	if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", parent.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "told"}}); err != nil {
		t.Fatal(err)
	}
	child := nextFor(t, h, "web-1")
	if child.Run == nil || child.Run.TaskID.String() != first.Deliveries[0].TaskID {
		t.Fatalf("child = %+v", child.Run)
	}
	back := emit(t, h, "web-1", child.ExecutionID, EmitEventCommand{Name: "listo.uno", Message: "done"})
	if len(back.Deliveries) != 1 || back.Deliveries[0].BotID != "recepcion" || back.Deliveries[0].Team != "tienda" || len(back.Skipped) != 0 {
		t.Errorf("back = %+v", back)
	}
}

func TestATeamWithNobodyTakingTasksIsUnavailable(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := shopTeam(t, h)
	subscribe(t, h, "caja", teamTopic())
	if _, err := NewPauseBot(h.deps).Execute(ctx, "caja"); err != nil {
		t.Fatal(err)
	}
	answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "x"})
	if len(answer.Deliveries) != 0 || len(answer.Skipped) != 1 || answer.Skipped[0].Reason != domain.RefusedTeamUnavailable || answer.Skipped[0].Team != "tienda" {
		t.Errorf("answer = %+v", answer)
	}
}

func TestAMemberNoNodeMayRunIsPassedOver(t *testing.T) {
	h := newHarness()
	parent := shopTeam(t, h)
	elsewhere := func(d *BotDefinition) { d.Placement = domain.PlacementSnapshot{Nodes: []string{"web-9"}} }
	teamBot(t, h, "aaa-elsewhere", []string{"tienda"}, elsewhere)
	subscribe(t, h, "aaa-elsewhere", teamTopic())
	subscribe(t, h, "caja", teamTopic())
	answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "x"})
	if len(answer.Deliveries) != 1 || answer.Deliveries[0].BotID != "caja" {
		t.Errorf("answer = %+v", answer)
	}
}

func TestTheOperatorsTeamEventGoesToOneMember(t *testing.T) {
	h := newHarness()
	shopTeamListens(t, h)
	subscribe(t, h, "almacen", domain.SubscriptionSnapshot{Topic: "pedido.*", Mode: "task", From: []string{"operator"}, Deliver: "team"})
	subscribe(t, h, "caja", domain.SubscriptionSnapshot{Topic: "pedido.*", Mode: "task", From: []string{"operator"}, Deliver: "team"})
	answer, err := NewPublishGroupEvent(h.deps).Execute(context.Background(), PublishGroupEventCommand{Group: "tienda", Name: "pedido.nuevo", Message: "from the counter", IdempotencyKey: "counter-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(answer.Deliveries) != 1 || answer.Deliveries[0].BotID != "almacen" || answer.Deliveries[0].Team != "tienda" {
		t.Errorf("answer = %+v", answer)
	}
}
