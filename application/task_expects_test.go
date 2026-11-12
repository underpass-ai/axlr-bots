package application

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// expectingTopic is teamTopic with the finals a task of it owes.
func expectingTopic() domain.SubscriptionSnapshot {
	sub := teamTopic()
	sub.Expects = []string{"pedido.listo", "pedido.flaky"}
	return sub
}

// shopTeamExpects is shopTeam with almacen, caja and supervisor on a team
// subscription to pedido.* that expects pedido.listo or pedido.flaky.
func shopTeamExpects(t *testing.T, h *harness) Assignment {
	t.Helper()
	parent := shopTeam(t, h)
	for _, id := range []string{"almacen", "caja", "supervisor"} {
		subscribe(t, h, id, expectingTopic())
	}
	return parent
}

func order(id string) json.RawMessage { return json.RawMessage(`{"id":"` + id + `"}`) }

// finish ends the assignment's execution Completed in session.
func finish(t *testing.T, h *harness, a Assignment, session string) {
	t.Helper()
	if _, err := NewReportExecution(h.deps).Execute(context.Background(), "web-1", a.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "ok", SessionID: session}}); err != nil {
		t.Fatal(err)
	}
}

func task(t *testing.T, h *harness, id domain.TaskID) domain.Task {
	t.Helper()
	got, err := h.tasks.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// redeliveryOfTask finds the task the key redeliver:<id> made, if any.
func redeliveryOfTask(t *testing.T, h *harness, id domain.TaskID) (domain.Task, bool) {
	t.Helper()
	all, err := h.tasks.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var found []domain.Task
	for _, candidate := range all {
		if candidate.IdempotencyKey().String() == RedeliveryKeyPrefix+id.String() {
			found = append(found, candidate)
		}
	}
	if len(found) > 1 {
		t.Fatalf("%d redeliveries of %s", len(found), id)
	}
	if len(found) == 0 {
		return domain.Task{}, false
	}
	return found[0], true
}

// twoOrders makes the team's task of p1 with p2 coalesced into it, and
// starts it on web-1.
func twoOrders(t *testing.T, h *harness) (Assignment, Assignment) {
	t.Helper()
	parent := shopTeamExpects(t, h)
	first := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "one", Data: order("p1")})
	second := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "two", Data: order("p2")})
	if second.Deliveries[0].CoalescedInto != first.Deliveries[0].TaskID {
		t.Fatalf("second = %+v", second.Deliveries)
	}
	finish(t, h, parent, "")
	run := nextFor(t, h, "web-1")
	if run.Run.TaskID.String() != first.Deliveries[0].TaskID {
		t.Fatalf("run = %s, want %s", run.Run.TaskID, first.Deliveries[0].TaskID)
	}
	return parent, run
}

func TestATaskThatSettlesOneOfTwoCarriedEventsRedeliversTheOther(t *testing.T) {
	h := newHarness()
	_, run := twoOrders(t, h)
	carried := task(t, h, run.Run.TaskID).Carried()
	if len(carried) != 2 || carried[0].ID != "p1" || carried[1].ID != "p2" || carried[1].Message != "two" || carried[1].From != "recepcion" {
		t.Fatalf("carried = %+v", carried)
	}
	emit(t, h, "web-1", run.ExecutionID, EmitEventCommand{Name: "pedido.listo", Message: "p1 done", Data: order("p1")})
	// A final of the wrong id, and an event outside expects, do not count.
	emit(t, h, "web-1", run.ExecutionID, EmitEventCommand{Name: "pedido.listo", Message: "p9 done", Data: order("p9")})
	emit(t, h, "web-1", run.ExecutionID, EmitEventCommand{Name: "pedido.visto", Message: "p2 seen", Data: order("p2")})
	if finals := task(t, h, run.Run.TaskID).Finals(); len(finals) != 1 || finals[0].ID != "p1" {
		t.Fatalf("finals = %+v", finals)
	}
	finish(t, h, run, "sess-1")
	again, ok := redeliveryOfTask(t, h, run.Run.TaskID)
	if !ok {
		t.Fatal("no redelivery")
	}
	if again.BotID() != "almacen" || again.Trigger() != task(t, h, run.Run.TaskID).Trigger() || again.Status() != domain.TaskPending || again.CorrelationID() != task(t, h, run.Run.TaskID).CorrelationID() {
		t.Errorf("redelivery = %+v", again.Snapshot())
	}
	if got := again.Carried(); len(got) != 1 || got[0].ID != "p2" || got[0].Redeliveries != 1 || len(got[0].Tasks) != 1 || got[0].Tasks[0] != run.Run.TaskID.String() {
		t.Errorf("redelivered carried = %+v", got)
	}
	var input struct {
		Name string         `json:"name"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(again.Input(), &input); err != nil || input.Name != "pedido.nuevo" || input.Data["id"] != "p2" || input.Data["context"] != "ws:sess-1" {
		t.Errorf("input = %s", again.Input())
	}
	if redelivery, ok := redeliveryOf(again); !ok || redelivery.Round != 1 || redelivery.Of != run.Run.TaskID.String() || redelivery.Context != "ws:sess-1" || redelivery.ContextTeam != "tienda" {
		t.Errorf("redelivery = %+v", redelivery)
	}
	for _, want := range []string{"Redelivery 1 of 2", "pedido.listo, pedido.flaky", "pedido.nuevo, data.id p2, from recepcion: two", "under ws:sess-1", "The earlier task's objective:"} {
		if !strings.Contains(again.Objective().String(), want) {
			t.Errorf("objective lacks %q:\n%s", want, again.Objective())
		}
	}
	if !hasAudit(h, "task.redeliver", domain.AuditOK) {
		t.Error("no task.redeliver audit")
	}
	// The redelivery's run emits the second final, as a flaky outcome:
	// nothing more.
	second := nextFor(t, h, "web-1")
	if second.Run.TaskID != again.ID() {
		t.Fatalf("second run = %s", second.Run.TaskID)
	}
	// The node is told the key and the team whose store holds it.
	if refs := second.Run.Contexts; len(refs) != 1 || refs[0] != (domain.ContextRef{Key: "ws:sess-1", Team: "tienda"}) {
		t.Errorf("contexts = %+v", refs)
	}
	emit(t, h, "web-1", second.ExecutionID, EmitEventCommand{Name: "pedido.flaky", Message: "p2 flaky", Data: order("p2")})
	read := []domain.ContextRead{{Key: "ws:sess-1", Team: "tienda", Read: []string{"p1 done"}}}
	if _, err := NewReportExecution(h.deps).Execute(context.Background(), "web-1", second.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "ok", SessionID: "sess-2"}, ContextRead: read}); err != nil {
		t.Fatal(err)
	}
	if earlier := task(t, h, again.ID()).Earlier(); len(earlier) != 1 || earlier[0].Read[0] != "p1 done" {
		t.Errorf("earlier = %+v", earlier)
	}
	if _, ok := redeliveryOfTask(t, h, again.ID()); ok {
		t.Error("a settled redelivery was redelivered")
	}
}

func TestAnEventGivenAgainTwiceWarnsOnTheThirdMiss(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := shopTeamExpects(t, h)
	subscribe(t, h, "recepcion", domain.SubscriptionSnapshot{Topic: "warn.incomplete", Mode: "note", From: []string{"control-plane"}})
	emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "one", Data: order("p1")})
	finish(t, h, parent, "")
	run := nextFor(t, h, "web-1")
	ids := []domain.TaskID{run.Run.TaskID}
	for round := 1; round <= domain.MaxRedeliveries; round++ {
		finish(t, h, run, "")
		again, ok := redeliveryOfTask(t, h, run.Run.TaskID)
		if !ok {
			t.Fatalf("round %d: no redelivery", round)
		}
		if got := again.Carried(); len(got) != 1 || got[0].Redeliveries != round {
			t.Fatalf("round %d: carried = %+v", round, got)
		}
		run = nextFor(t, h, "web-1")
		ids = append(ids, run.Run.TaskID)
	}
	finish(t, h, run, "")
	if _, ok := redeliveryOfTask(t, h, run.Run.TaskID); ok {
		t.Fatal("a third redelivery")
	}
	count := func() int {
		n := 0
		for _, event := range h.log.events {
			if event.Type == domain.EventEventEmitted && event.Payload["name"] == "warn.incomplete" {
				n++
			}
		}
		return n
	}
	if count() != 1 {
		t.Fatalf("warn.incomplete published %d times", count())
	}
	if !hasAudit(h, "task.incomplete", domain.AuditDenied) {
		t.Error("no task.incomplete audit")
	}
	heard, ok := lastEvent(h, domain.EventEventHeard)
	if !ok || heard.BotID != "recepcion" || heard.Payload["name"] != "warn.incomplete" || heard.Payload["by"] != "control-plane" {
		t.Errorf("heard = %+v", heard)
	}
	emitted, _ := lastEvent(h, domain.EventEventEmitted)
	data, _ := emitted.Payload["data"].(map[string]any)
	if data["left"] != float64(1) || data["subscription"] != "pedido.*" || !strings.Contains(asJSON(t, data["tasks"]), ids[0].String()) || !strings.Contains(asJSON(t, data["events"]), `"id":"p1"`) {
		t.Errorf("data = %s", asJSON(t, data))
	}
	// The same end checked again publishes nothing twice.
	bot, _ := h.bots.Get(ctx, task(t, h, run.Run.TaskID).BotID())
	if err := newCompletionKeeper(h.deps).taskEnded(ctx, bot, task(t, h, run.Run.TaskID)); err != nil {
		t.Fatal(err)
	}
	if count() != 1 {
		t.Errorf("warn.incomplete published %d times after a repeat", count())
	}
}

func TestACrashBetweenTheEndAndTheRedeliveryMakesNoSecond(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	_, run := twoOrders(t, h)
	finish(t, h, run, "")
	first, ok := redeliveryOfTask(t, h, run.Run.TaskID)
	if !ok {
		t.Fatal("no redelivery")
	}
	ended := task(t, h, run.Run.TaskID)
	bot, _ := h.bots.Get(ctx, ended.BotID())
	// The end is checked again, as after a crash: the key finds the task,
	// whoever holds it.
	if err := newCompletionKeeper(h.deps).taskEnded(ctx, bot, ended); err != nil {
		t.Fatal(err)
	}
	if again, _ := redeliveryOfTask(t, h, run.Run.TaskID); again.ID() != first.ID() {
		t.Errorf("redelivery %s, then %s", first.ID(), again.ID())
	}
}

func TestARedeliveryHeldByAnotherMemberIsFound(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	_, run := twoOrders(t, h)
	// The redelivery was made for caja before a crash, its event never
	// recorded.
	if _, err := NewSubmitTask(h.deps).Execute(ctx, SubmitTaskCommand{BotID: "caja", Objective: "the redelivery", IdempotencyKey: RedeliveryKeyPrefix + run.Run.TaskID.String()}); err != nil {
		t.Fatal(err)
	}
	finish(t, h, run, "")
	if again, _ := redeliveryOfTask(t, h, run.Run.TaskID); again.BotID() != "caja" {
		t.Errorf("redelivery = %s", again.BotID())
	}
}

func TestWithoutExpectsNothingIsRecordedOrRedelivered(t *testing.T) {
	h := newHarness()
	parent := shopTeamListens(t, h)
	first := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "one", Data: order("p1")})
	emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "two", Data: order("p2")})
	finish(t, h, parent, "")
	run := nextFor(t, h, "web-1")
	made := task(t, h, domain.TaskID(first.Deliveries[0].TaskID))
	if made.Snapshot().Expects != nil || len(made.Carried()) != 0 {
		t.Errorf("expects = %+v", made.Snapshot().Expects)
	}
	raw, _ := json.Marshal(made.Snapshot())
	if strings.Contains(string(raw), `"expects"`) {
		t.Errorf("snapshot = %s", raw)
	}
	emit(t, h, "web-1", run.ExecutionID, EmitEventCommand{Name: "pedido.listo", Message: "done", Data: order("p1")})
	finish(t, h, run, "s")
	if _, ok := redeliveryOfTask(t, h, run.Run.TaskID); ok {
		t.Error("a task without expects was redelivered")
	}
}

func TestAFullTaskTakesNoMoreNotes(t *testing.T) {
	h := newHarness()
	parent := shopTeamExpects(t, h)
	first := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "0", Data: order("p0")})
	made := domain.TaskID(first.Deliveries[0].TaskID)
	for i := 1; i < domain.MaxCarriedEvents; i++ {
		// Past the emitter's rate and the receiver's intake.
		h.clock.now = h.clock.now.Add(time.Minute)
		emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "n", Data: order("p" + string(rune('a'+i%26)) + string(rune('a'+i/26)))})
	}
	if got := len(task(t, h, made).Carried()); got != domain.MaxCarriedEvents {
		t.Fatalf("carried = %d", got)
	}
	h.clock.now = h.clock.now.Add(time.Minute)
	next := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "last", Data: order("last")})
	if d := next.Deliveries[0]; d.Mode != "task" || d.TaskID == string(made) {
		t.Errorf("delivery = %+v", d)
	}
}

func TestACrossedEventIsCarriedAndRedeliveredWithItsOrigin(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := qaAndDev(t, h)
	sub := hearsFrom("incidencia.nueva", "qa")
	sub.Expects = []string{"incidencia.completada"}
	subscribe(t, h, "dev-arreglo", sub)
	emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "incidencia.nueva", Message: "a finding", Data: finding("h-1")})
	tasks := tasksOf(t, h, "dev-arreglo")
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d", len(tasks))
	}
	made := tasks[0]
	if carried := made.Carried(); len(carried) != 1 || carried[0].ID != "h-1" || carried[0].From != "qa-triaje" {
		t.Fatalf("carried = %+v", carried)
	}
	bot, _ := h.bots.Get(ctx, "dev-arreglo")
	// It ends Failed without its final: given again with its origin.
	stored := made.Snapshot()
	stored.Status, stored.FailureReason, stored.ExecutionIDs = "Failed", "runtime: boom", nil
	failed, err := domain.RestoreTask(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := newCompletionKeeper(h.deps).taskEnded(ctx, bot, failed); err != nil {
		t.Fatal(err)
	}
	again, ok := redeliveryOfTask(t, h, made.ID())
	if !ok {
		t.Fatal("no redelivery")
	}
	origin, crossed := again.Origin()
	if !crossed || origin.Context != "project:qa.hallazgo.h-1" || again.Carried()[0].ID != "h-1" {
		t.Errorf("redelivery = %+v", again.Snapshot())
	}
	if _, has := redeliveryOf(again); !has {
		t.Error("no input.redelivery")
	}
	// Its run is told the other team's key (the node says to ask them);
	// the run of the Failed task left no session, so no key of its own.
	if refs := contextRefsOf(again); len(refs) != 1 || refs[0] != (domain.ContextRef{Key: "project:qa.hallazgo.h-1", Team: "qa"}) {
		t.Errorf("contexts = %+v", refs)
	}
}

func TestOnlyAStuckCancelIsChecked(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	_, run := twoOrders(t, h)
	running := task(t, h, run.Run.TaskID)
	bot, _ := h.bots.Get(ctx, running.BotID())
	keeper := newCompletionKeeper(h.deps)
	if err := keeper.taskStuck(ctx, bot, running); err != nil {
		t.Fatal(err)
	}
	if err := keeper.taskEnded(ctx, bot, running); err != nil {
		t.Fatal(err)
	}
	if _, ok := redeliveryOfTask(t, h, running.ID()); ok {
		t.Fatal("a running task was redelivered")
	}
	cancelled := running
	if err := cancelled.CancelBecause("stuck, judged by a watcher", t0); err != nil {
		t.Fatal(err)
	}
	if err := keeper.taskEnded(ctx, bot, cancelled); err != nil {
		t.Fatal(err)
	}
	if _, ok := redeliveryOfTask(t, h, running.ID()); ok {
		t.Fatal("a cancelled task was redelivered by its end")
	}
	if err := keeper.taskStuck(ctx, bot, cancelled); err != nil {
		t.Fatal(err)
	}
	again, ok := redeliveryOfTask(t, h, running.ID())
	if !ok || len(again.Carried()) != 2 {
		t.Fatalf("stuck: redelivery %v %+v", ok, again.Carried())
	}
}

func TestAnOperatorsCancelDropsTheWork(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	_, run := twoOrders(t, h)
	if _, err := NewCancelTask(h.deps).Execute(ctx, run.Run.TaskID); err != nil {
		t.Fatal(err)
	}
	if _, ok := redeliveryOfTask(t, h, run.Run.TaskID); ok {
		t.Error("a cancelled task was redelivered")
	}
}

func TestTheRedeliveryObjectiveFitsThePrompt(t *testing.T) {
	h := newHarness()
	_, run := twoOrders(t, h)
	ended := task(t, h, run.Run.TaskID)
	bot, _ := h.bots.Get(context.Background(), ended.BotID())
	many := make([]domain.CarriedEvent, maxIncompleteListed+3)
	for i := range many {
		many[i] = domain.CarriedEvent{EventID: "evt-1", Name: "pedido.nuevo", From: "recepcion", Message: strings.Repeat("x", 280)}
	}
	objective, err := redeliveryObjective(bot, ended, many, 1, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	reserve := domain.ContextRoom([]domain.ContextRef{{Key: "ws:s1", Team: "tienda"}}, "tienda")
	leaving, err := redeliveryObjective(bot, ended, many, 1, "ws:s1", reserve)
	if err != nil {
		t.Fatal(err)
	}
	if len(leaving) > domain.ObjectiveRoomBeside(bot.Mission())-reserve || domain.MaxPromptLength-len(domain.Prompt("")) < len(leaving)+reserve {
		t.Errorf("the objective (%d) leaves no room for the read (%d)", len(leaving), reserve)
	}
	if len(objective) > domain.ObjectiveRoomBeside(bot.Mission()) || !strings.Contains(objective.String(), "and 3 more") || !strings.Contains(objective.String(), "(none: any final event settles it)") {
		t.Errorf("objective (%d) = %s", len(objective), objective)
	}
}

func TestTheContextIsHandedOnWhenTheRunLeftNoSession(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	_, run := twoOrders(t, h)
	finish(t, h, run, "sess-9")
	first, _ := redeliveryOfTask(t, h, run.Run.TaskID)
	second := nextFor(t, h, "web-1")
	finish(t, h, second, "")
	again, ok := redeliveryOfTask(t, h, first.ID())
	if !ok {
		t.Fatal("no second redelivery")
	}
	if redelivery, _ := redeliveryOf(again); redelivery.Context != "ws:sess-9" || redelivery.Round != 2 {
		t.Errorf("redelivery = %+v", redelivery)
	}
	if !strings.Contains(again.Objective().String(), "Redelivery 2 of 2") {
		t.Errorf("objective = %s", again.Objective())
	}
	_ = ctx
}

func TestAReportsBadContextReadIsDropped(t *testing.T) {
	h := newHarness()
	_, run := twoOrders(t, h)
	bad := []domain.ContextRead{{Key: "no key", Team: "tienda"}}
	if _, err := NewReportExecution(h.deps).Execute(context.Background(), "web-1", run.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "ok"}, ContextRead: bad}); err != nil {
		t.Fatal(err)
	}
	if earlier := task(t, h, run.Run.TaskID).Earlier(); len(earlier) != 0 {
		t.Errorf("earlier = %+v", earlier)
	}
	if refs := contextRefsOf(task(t, h, run.Run.TaskID)); refs != nil {
		t.Errorf("a plain event task has contexts: %+v", refs)
	}
}

func TestARedeliveryLeavesItsPromptRoomForWhatTheNodeReads(t *testing.T) {
	h := newHarness()
	parent := shopTeam(t, h)
	long := expectingTopic()
	long.Objective = strings.Repeat("Pack the order with care. ", 200) + "{{.message}}"
	for _, id := range []string{"almacen", "caja", "supervisor"} {
		subscribe(t, h, id, long)
	}
	emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "pedido.nuevo", Message: "one", Data: order("p1")})
	finish(t, h, parent, "")
	run := nextFor(t, h, "web-1")
	finish(t, h, run, "sess-long")
	again := nextFor(t, h, "web-1")
	refs := again.Run.Contexts
	if len(refs) != 1 {
		t.Fatalf("contexts = %+v", refs)
	}
	reserve := domain.ContextRoom(refs, "tienda")
	if prompt := again.Run.Prompt.String(); len(prompt)+reserve > domain.MaxPromptLength || !strings.Contains(prompt, "Redelivery 1 of 2") {
		t.Errorf("prompt %d + reserve %d > %d", len(prompt), reserve, domain.MaxPromptLength)
	}
}

func TestAFailedRunHasWhatIsMissingGivenAgain(t *testing.T) {
	h := newHarness()
	_, run := twoOrders(t, h)
	if _, err := NewReportExecution(h.deps).Execute(context.Background(), "web-1", run.ExecutionID, ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "runtime: boom"}); err != nil {
		t.Fatal(err)
	}
	if again, ok := redeliveryOfTask(t, h, run.Run.TaskID); !ok || len(again.Carried()) != 2 || !strings.Contains(again.Objective().String(), "ended failed") {
		t.Errorf("redelivery %v: %+v", ok, again.Snapshot())
	}
}

func TestATaskJudgedStuckHasWhatIsMissingGivenAgain(t *testing.T) {
	h, parent, watcher := watchingWork(t)
	ctx := context.Background()
	// The running task carries an event it owes a final for.
	running := task(t, h, parent.Run.TaskID)
	stored := running.Snapshot()
	stored.Expects = &domain.TaskExpectsSnapshot{Names: []string{"qa.hallazgo.triado"}, Carried: []domain.CarriedEvent{{EventID: "evt-1", Name: "qa.hallazgo.candidato", ID: "h-1"}}}
	carrying, err := domain.RestoreTask(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.tasks.Save(ctx, carrying); err != nil {
		t.Fatal(err)
	}
	h.clock.now = t0.Add(6 * time.Minute)
	if decided := decideWatch(t, h, watcher.ExecutionID, WatchDecision{Question: running.ID(), Choice: "stuck", Probability: 0.9, Confidence: 0.8}); !decided.Stuck {
		t.Fatalf("decided = %+v", decided)
	}
	again, ok := redeliveryOfTask(t, h, running.ID())
	if !ok || again.BotID() != running.BotID() || again.Carried()[0].ID != "h-1" || !strings.Contains(again.Objective().String(), "ended cancelled") {
		t.Errorf("redelivery %v: %+v", ok, again.Snapshot())
	}
}

func TestAPlainCrossedTasksRunIsToldTheOtherTeamsKey(t *testing.T) {
	// The one change without expects: E8d hands the node the key another
	// team's domain event named, so the run is told to ask that team.
	h := newHarness()
	parent := qaAndDev(t, h)
	emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "incidencia.nueva", Message: "a finding", Data: finding("h-1")})
	finish(t, h, parent, "")
	run := nextFor(t, h, "web-1")
	if refs := run.Run.Contexts; len(refs) != 1 || refs[0] != (domain.ContextRef{Key: "project:qa.hallazgo.h-1", Team: "qa"}) {
		t.Errorf("contexts = %+v", refs)
	}
	if made := task(t, h, run.Run.TaskID); made.Snapshot().Expects != nil {
		t.Errorf("a crossed task without expects records %+v", made.Snapshot().Expects)
	}
}
