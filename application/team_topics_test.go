package application

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// docs/architecture/team-topics.md, cut 3: delivery across teams.

// emitsTo lets a bot emit the name to the teams, beside info.* in its own.
func emitsTo(name string, teams ...string) func(*BotDefinition) {
	return func(d *BotDefinition) {
		d.Events = &domain.EventsSnapshot{Emit: []domain.EmitRuleSnapshot{{Name: "info.*"}, {Name: name, Teams: teams}}}
	}
}

// hearsFrom is a team subscription to the topic from the other team.
func hearsFrom(topic, team string) domain.SubscriptionSnapshot {
	return domain.SubscriptionSnapshot{Topic: topic, Team: team, Mode: "task", Deliver: "team", Objective: "Team {{.team}} has news. Read " + "{{.id}} in your own programs."}
}

// bothTeams is the ADR's case: b-triage and b-reviewer in team-b publish
// order.new to team-a; a-fixer in team-a hears it from team-b as a team,
// a-other hears nothing; b-triage runs a task on web-1.
func bothTeams(t *testing.T, h *harness) Assignment {
	t.Helper()
	onlineNode(t, h, "web-1")
	triaje := teamBot(t, h, "b-triage", []string{"team-b"}, emitsTo("order.new", "team-a"))
	teamBot(t, h, "b-reviewer", []string{"team-b"}, emitsTo("order.new", "team-a"))
	teamBot(t, h, "a-fixer", []string{"team-a"}, emitsTo("order.completed", "team-b"))
	teamBot(t, h, "a-other", []string{"team-a"}, nil)
	subscribe(t, h, "a-fixer", hearsFrom("order.new", "team-b"))
	return runOnNode(t, h, triaje, "web-1", "ask-b-1")
}

func finding(id string) json.RawMessage {
	return json.RawMessage(`{"id": "` + id + `", "context": "project:team-b.item.` + id + `"}`)
}

// tasksOf lists the bot's tasks made by another team's event.
func tasksOf(t *testing.T, h *harness, bot string) []domain.Task {
	t.Helper()
	var out []domain.Task
	for _, status := range []domain.TaskStatus{domain.TaskPending, domain.TaskRunning, domain.TaskCompleted} {
		tasks, err := h.tasks.ListByStatus(context.Background(), status)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range tasks {
			if _, crossed := task.Origin(); crossed && task.BotID().String() == bot {
				out = append(out, task)
			}
		}
	}
	return out
}

func TestTeamBsItemMakesOneTaskInTeamA(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := bothTeams(t, h)
	answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "order.new", Message: "a finding", Data: finding("h-1")})
	if answer.Refused != nil || len(answer.Deliveries) != 1 || len(answer.Skipped) != 0 {
		t.Fatalf("answer = %+v", answer)
	}
	if d := answer.Deliveries[0]; d.Team != "team-a" || !d.Crossed || d.BotID != "" || d.TaskID != "" || d.Mode != "task" || !d.Created || d.Trigger != "order.new" {
		t.Errorf("delivery = %+v", d)
	}
	if emitted, ok := lastEvent(h, domain.EventEventEmitted); !ok || !strings.Contains(asJSON(t, emitted.Payload["deliveries"]), `"crossed":true,`) || !strings.Contains(asJSON(t, emitted.Payload["deliveries"]), `"team":"team-a"`) {
		t.Errorf("emitted = %+v", emitted.Payload["deliveries"])
	}
	tasks := tasksOf(t, h, "a-fixer")
	if len(tasks) != 1 || len(tasksOf(t, h, "a-other")) != 0 {
		t.Fatalf("tasks = %d", len(tasks))
	}
	task := tasks[0]
	if task.Depth() != 0 {
		t.Errorf("depth = %d", task.Depth())
	}
	if _, linked := task.Lineage(); linked {
		t.Error("a crossed task has lineage")
	}
	origin, _ := task.Origin()
	event, _ := domain.NewDomainEvent("order.new", "h-1", "project:team-b.item.h-1", "team-b", "b-triage", t0)
	if origin.Crossings != 1 || origin.Context != "project:team-b.item.h-1" || origin.Team != "team-b" || origin.From != "b-triage" || origin.EventID != event.Identity() || origin.TraceID != parent.Run.CorrelationID.String() {
		t.Errorf("origin = %+v (trace %s)", origin, parent.Run.CorrelationID)
	}
	if task.IdempotencyKey().String() != "event:"+event.Identity()+":team:team-a" || task.Trigger().Name != "order.new" || task.IsUrgent() {
		t.Errorf("task = %s %+v", task.IdempotencyKey(), task.Trigger())
	}
	objective := task.Objective().String()
	if !strings.HasPrefix(objective, "Team team-b has news. Read h-1 in your own programs.\n\n"+domain.CrossedEventHeading+"\n{") || !strings.Contains(objective, `"context":"project:team-b.item.h-1"`) {
		t.Errorf("objective = %q", objective)
	}
	heard, ok := lastEvent(h, domain.EventEventHeard)
	if !ok || heard.BotID != "a-fixer" || heard.Payload["fromTeam"] != "team-b" || heard.Payload["crossed"] != true || heard.Payload["team"] != "team-a" || heard.Payload["mode"] != "task" {
		t.Errorf("heard = %+v", heard)
	}
	if entry := lastCross(h); entry.Outcome != domain.AuditOK || entry.Details["team"] != "team-a" || entry.Details["bot"] != "a-fixer" {
		t.Errorf("audit = %+v", entry)
	}
	if again := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "order.new", Message: "a finding", Data: finding("h-1")}); !again.Deduplicated || len(again.Deliveries) != 1 || !again.Deliveries[0].Crossed || len(h.bus.published) != 1 {
		t.Errorf("again = %+v, published %d", again, len(h.bus.published))
	}
	if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", parent.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "triaged"}}); err != nil {
		t.Fatal(err)
	}
}

func asJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func lastCross(h *harness) domain.AuditEntry {
	entry, _ := lastAudit(h, "event.cross")
	return entry
}

func TestThreeFindingsAreThreeTasksStartedOldestFirst(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := bothTeams(t, h)
	var made []string
	for i, id := range []string{"h-1", "h-2", "h-3"} {
		h.clock.now = t0.Add(time.Duration(i+1) * time.Second)
		answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "order.new", Message: "finding " + id, Data: finding(id)})
		if len(answer.Deliveries) != 1 || !answer.Deliveries[0].Crossed {
			t.Fatalf("%s: %+v", id, answer)
		}
		made = append(made, h.bus.answers[i].Deliveries[0].TaskID)
	}
	if len(tasksOf(t, h, "a-fixer")) != 3 || made[0] == made[1] || made[1] == made[2] {
		t.Fatalf("made = %v", made)
	}
	// Nothing coalesced: each is a task, none a note.
	for _, answer := range h.bus.answers {
		if d := answer.Deliveries[0]; d.Mode != "task" || d.CoalescedInto != "" || !d.Created {
			t.Errorf("delivery = %+v", d)
		}
	}
	if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", parent.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "triaged"}}); err != nil {
		t.Fatal(err)
	}
	for _, want := range made {
		next := nextFor(t, h, "web-1")
		if next.Run == nil || next.Run.TaskID.String() != want {
			t.Fatalf("started %+v, want %s", next.Run, want)
		}
		if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", next.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "fixed"}}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTheSameItemFromTwoTeamBMembersIsOneTask(t *testing.T) {
	h := newHarness()
	first := bothTeams(t, h)
	onlineNode(t, h, "web-2")
	revisor, _ := h.bots.Get(context.Background(), "b-reviewer")
	second := runOnNode(t, h, revisor, "web-2", "ask-b-2")
	if second.Run == nil || second.Run.BotID != "b-reviewer" {
		t.Fatalf("second = %+v", second.Run)
	}
	a := emit(t, h, "web-1", first.ExecutionID, EmitEventCommand{Name: "order.new", Message: "found it", Data: finding("h-7")})
	b := emit(t, h, "web-2", second.ExecutionID, EmitEventCommand{Name: "order.new", Message: "found it too", Data: finding("h-7")})
	// The two emits are two events of their team; the fact they carry
	// across is one, whoever of team-b published it.
	if a.EventID == b.EventID || len(h.bus.published) != 2 || h.bus.published[0].Event.Identity() != h.bus.published[1].Event.Identity() {
		t.Fatalf("a %s, b %s, published %d", a.EventID, b.EventID, len(h.bus.published))
	}
	if got := tasksOf(t, h, "a-fixer"); len(got) != 1 {
		t.Errorf("tasks = %d", len(got))
	}
	if d := h.bus.answers[1].Deliveries; len(d) != 1 || d[0].Created || d[0].TaskID != h.bus.answers[0].Deliveries[0].TaskID {
		t.Errorf("repeat = %+v", d)
	}
}

func TestOneSideAloneDeliversNothing(t *testing.T) {
	t.Run("no emit rule names team-a", func(t *testing.T) {
		h := newHarness()
		onlineNode(t, h, "web-1")
		triaje := teamBot(t, h, "b-triage", []string{"team-b"}, nil)
		teamBot(t, h, "a-fixer", []string{"team-a"}, nil)
		subscribe(t, h, "a-fixer", hearsFrom("order.new", "team-b"))
		parent := runOnNode(t, h, triaje, "web-1", "ask-1")
		answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "order.new", Message: "x", Data: finding("h-1")})
		if len(answer.Deliveries) != 0 || len(answer.Skipped) != 0 || len(h.bus.published) != 0 || len(tasksOf(t, h, "a-fixer")) != 0 {
			t.Errorf("answer = %+v", answer)
		}
	})
	t.Run("nobody in team-a subscribes", func(t *testing.T) {
		h := newHarness()
		onlineNode(t, h, "web-1")
		triaje := teamBot(t, h, "b-triage", []string{"team-b"}, emitsTo("order.new", "team-a"))
		teamBot(t, h, "a-fixer", []string{"team-a"}, nil)
		parent := runOnNode(t, h, triaje, "web-1", "ask-1")
		emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "order.new", Message: "x", Data: finding("h-1")})
		if len(tasksOf(t, h, "a-fixer")) != 0 || len(h.bus.answers) != 1 {
			t.Fatal("a task without a listener")
		}
		if s := h.bus.answers[0].Skipped; len(s) != 1 || s[0].Reason != domain.RefusedTeamUnavailable || s[0].Team != "team-a" || !s[0].Crossed {
			t.Errorf("skipped = %+v", s)
		}
		if entry := lastCross(h); entry.Outcome != domain.AuditDenied || entry.Details["reason"] != domain.RefusedTeamUnavailable {
			t.Errorf("audit = %+v", entry)
		}
	})
}

func TestWhatDoesNotCross(t *testing.T) {
	h := newHarness()
	parent := bothTeams(t, h)
	addressed := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "order.new", Message: "x", Data: finding("h-1"), To: []string{"b-reviewer"}})
	if len(h.bus.published) != 0 {
		t.Errorf("addressed crossed: %+v", addressed)
	}
	noID := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "order.new", Message: "y", Data: json.RawMessage(`{"what": "no id"}`)})
	if len(noID.Skipped) != 1 || noID.Skipped[0].Reason != domain.RefusedNoDomainID || noID.Skipped[0].Team != "team-a" || len(h.bus.published) != 0 {
		t.Errorf("no id = %+v", noID)
	}
	if emitted, _ := lastEvent(h, domain.EventEventEmitted); !strings.Contains(asJSON(t, emitted.Payload["skipped"]), `"crossed":true`) {
		t.Errorf("skipped record = %+v", emitted.Payload["skipped"])
	}
	badContext := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "order.new", Message: "z", Data: json.RawMessage(`{"id": "h-2", "context": "not a key"}`)})
	if len(badContext.Skipped) != 1 || badContext.Skipped[0].Reason != domain.RefusedNoDomainID {
		t.Errorf("bad context = %+v", badContext)
	}
	h.deps.DomainEvents = nil
	noBus := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "order.new", Message: "w", Data: finding("h-3")})
	if len(noBus.Skipped) != 1 || noBus.Skipped[0].Reason != domain.RefusedTeamUnavailable || !noBus.Skipped[0].Crossed {
		t.Errorf("no bus = %+v", noBus)
	}
	h.deps.DomainEvents = h.bus
	h.bus.fail = errBoom
	if _, err := NewEmitEvent(h.deps).Execute(context.Background(), "web-1", parent.ExecutionID, EmitEventCommand{Name: "order.new", Message: "v", Data: finding("h-4")}); err == nil {
		t.Error("a bus that refused the publish was not answered")
	}
}

// teamAAnswers adds b-triage's subscription to team-a's order.completed.
func teamAAnswers(t *testing.T, h *harness) {
	t.Helper()
	subscribe(t, h, "b-triage", hearsFrom("order.completed", "team-a"))
}

func TestTeamAsFixReachesTeamBWithASecondCrossing(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := bothTeams(t, h)
	teamAAnswers(t, h)
	emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "order.new", Message: "x", Data: finding("h-1")})
	if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", parent.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "triaged"}}); err != nil {
		t.Fatal(err)
	}
	fix := nextFor(t, h, "web-1")
	if fix.Run == nil || fix.Run.BotID != "a-fixer" {
		t.Fatalf("fix = %+v", fix.Run)
	}
	answer := emit(t, h, "web-1", fix.ExecutionID, EmitEventCommand{Name: "order.completed", Message: "merged", Data: json.RawMessage(`{"id": "h-1@4f9c2ab"}`)})
	if len(answer.Deliveries) != 1 || answer.Deliveries[0].Team != "team-b" {
		t.Fatalf("answer = %+v", answer)
	}
	back := tasksOf(t, h, "b-triage")
	if len(back) != 1 {
		t.Fatalf("team-b tasks = %d", len(back))
	}
	if origin, _ := back[0].Origin(); origin.Crossings != 2 || origin.Team != "team-a" || origin.Context != "" {
		t.Errorf("origin = %+v", origin)
	}
}

func TestTheFifthCrossingIsRefused(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	onlineNode(t, h, "web-1")
	teamBot(t, h, "b-triage", []string{"team-b"}, emitsTo("order.new", "team-a"))
	teamBot(t, h, "a-fixer", []string{"team-a"}, nil)
	teamAAnswers(t, h)
	event, _ := domain.NewDomainEvent("order.completed", "h-1@abc", "", "team-a", "a-fixer", t0)
	audience, _ := domain.NewAudience("team-a", []domain.TeamName{"team-b"})
	// A trace that crossed four times already reaches team-b through the runner.
	received, err := NewSerialRunner(h.deps).ReceiveDomainEvent(ctx, CrossingEvent{Tenant: "default", Event: event, Audience: audience, TraceID: "trace-far", Crossings: domain.MaxTeamCrossings})
	if err != nil || len(received.Deliveries) != 1 {
		t.Fatalf("received = %+v %v", received, err)
	}
	run := nextFor(t, h, "web-1")
	if run.Run == nil || run.Run.CorrelationID != "trace-far" {
		t.Fatalf("run = %+v", run.Run)
	}
	answer := emit(t, h, "web-1", run.ExecutionID, EmitEventCommand{Name: "order.new", Message: "again", Data: finding("h-1")})
	if len(answer.Skipped) != 1 || answer.Skipped[0].Reason != domain.RefusedCrossingLimit || len(h.bus.published) != 0 {
		t.Errorf("answer = %+v", answer)
	}
}

// A fact is never lost while the listening team is Paused: the task waits
// Pending for its bot, and runs once the bot is enabled.
func TestAFactWaitsForAPausedTeam(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := bothTeams(t, h)
	if _, err := NewPauseBot(h.deps).Execute(ctx, "a-fixer"); err != nil {
		t.Fatal(err)
	}
	emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "order.new", Message: "x", Data: finding("h-1")})
	waiting := tasksOf(t, h, "a-fixer")
	if len(waiting) != 1 || waiting[0].Status() != domain.TaskPending {
		t.Fatalf("waiting = %+v", waiting)
	}
	if heard, _ := lastEvent(h, domain.EventEventHeard); heard.Payload["waits"] != "Paused" {
		t.Errorf("heard = %+v", heard.Payload)
	}
	if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", parent.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "triaged"}}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := NewNextAssignment(h.deps).Execute(ctx, "web-1"); err != nil || found {
		t.Fatalf("a Paused bot's task started: %v %v", found, err)
	}
	// The same fact again while it waits: still one task.
	emitAgain := h.bus.published[0]
	if again, err := NewReceiveDomainEvent(h.deps).Execute(ctx, emitAgain); err != nil || again.Deliveries[0].Created {
		t.Errorf("again = %+v %v", again, err)
	}
	if _, err := NewEnableBot(h.deps).Execute(ctx, "a-fixer"); err != nil {
		t.Fatal(err)
	}
	run := nextFor(t, h, "web-1")
	if run.Run == nil || run.Run.TaskID != waiting[0].ID() || len(tasksOf(t, h, "a-fixer")) != 1 {
		t.Errorf("run = %+v", run.Run)
	}
}

func TestAHearerOfItsOwnMakesItsOwnTask(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := bothTeams(t, h)
	alone := domain.SubscriptionSnapshot{Topic: "order.*", Team: "team-b", From: []string{"b-triage"}}
	subscribe(t, h, "a-other", alone)
	emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "order.new", Message: "x", Data: finding("h-1")})
	own := tasksOf(t, h, "a-other")
	if len(own) != 1 || len(tasksOf(t, h, "a-fixer")) != 1 {
		t.Fatalf("own = %d", len(own))
	}
	event := h.bus.published[0].Event
	if own[0].IdempotencyKey().String() != "event:"+event.Identity()+":a-other" || !strings.HasPrefix(own[0].Objective().String(), "Team team-b published order.new about h-1.") {
		t.Errorf("task = %s %q", own[0].IdempotencyKey(), own[0].Objective())
	}
	// A bot of another tenant, or of another team, hears nothing.
	if answer, err := NewReceiveDomainEvent(h.deps).Execute(ctx, CrossingEvent{Tenant: "elsewhere", Event: event, Audience: h.bus.published[0].Audience, Crossings: 1}); err != nil || len(answer.Deliveries) != 0 || len(answer.Skipped) != 1 {
		t.Errorf("elsewhere = %+v %v", answer, err)
	}
}

func TestAnObjectiveThatDoesNotRenderIsASkip(t *testing.T) {
	h := newHarness()
	parent := bothTeams(t, h)
	bad := hearsFrom("order.new", "team-b")
	bad.Objective = "{{.nope}}"
	subscribe(t, h, "a-fixer", bad)
	emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "order.new", Message: "x", Data: finding("h-1")})
	if s := h.bus.answers[0].Skipped; len(s) != 1 || s[0].Reason != "objective" || s[0].BotID != "a-fixer" {
		t.Errorf("skipped = %+v", s)
	}
}

// An event of the bot's own team never joins a crossed task as a note.
func TestNothingJoinsACrossedTask(t *testing.T) {
	h := newHarness()
	parent := bothTeams(t, h)
	subscribe(t, h, "a-fixer", hearsFrom("order.new", "team-b"), domain.SubscriptionSnapshot{Topic: "order.*", Mode: "task"})
	emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "order.new", Message: "x", Data: finding("h-1")})
	crossed := tasksOf(t, h, "a-fixer")[0]
	bot, _ := h.bots.Get(context.Background(), "a-fixer")
	name, _ := domain.NewEventName("order.new")
	if pending, found, err := pendingTaskOf(context.Background(), h.deps, bot, name); err != nil || found {
		t.Errorf("found %s (crossed %s)", pending.ID(), crossed.ID())
	}
}

// A subscription to another team names bots of that team in from, and a
// definition keeps teams and team on a round trip.
func TestDefinitionsKeepTheCrossingFields(t *testing.T) {
	h := newHarness()
	bothTeams(t, h)
	ok := domain.SubscriptionSnapshot{Topic: "order.new", Team: "team-b", From: []string{"b-triage", "b-reviewer"}, Deliver: "team"}
	bot := subscribe(t, h, "a-other", ok)
	if got := bot.Snapshot().Subscriptions; len(got) != 1 || got[0].Team != "team-b" || len(got[0].From) != 2 {
		t.Errorf("subscriptions = %+v", got)
	}
	teamB, _ := h.bots.Get(context.Background(), "b-triage")
	if rules := teamB.Snapshot().Events.Emit; len(rules) != 2 || len(rules[1].Teams) != 1 || rules[1].Teams[0] != "team-a" {
		t.Errorf("rules = %+v", rules)
	}
	for name, from := range map[string][]string{"a teammate": {"a-fixer"}, "the operator": {"operator"}, "nobody known": {"ghost"}} {
		bad := domain.SubscriptionSnapshot{Topic: "order.new", Team: "team-b", From: from}
		subs := []domain.SubscriptionSnapshot{bad}
		if _, err := NewUpdateBot(h.deps).Execute(context.Background(), "a-other", BotPatch{Subscriptions: &subs}); err == nil {
			t.Errorf("%s accepted in from", name)
		}
	}
}

// The crossing count follows the whole chain: a teammate's task that a
// crossed task's event made (a lineage, no origin of its own) counts the
// trace's crossings, so the limit cannot be reset inside a team.
func TestATeammatesTaskCountsTheTracesCrossings(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := bothTeams(t, h)
	teamAAnswers(t, h)
	teamBot(t, h, "a-reviewer", []string{"team-a"}, emitsTo("order.completed", "team-b"))
	subscribe(t, h, "a-reviewer", domain.SubscriptionSnapshot{Topic: "info.listo", Mode: "task"})
	emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "order.new", Message: "x", Data: finding("h-1")})
	if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", parent.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "triaged"}}); err != nil {
		t.Fatal(err)
	}
	fix := nextFor(t, h, "web-1")
	if fix.Run == nil || fix.Run.BotID != "a-fixer" {
		t.Fatalf("fix = %+v", fix.Run)
	}
	if answer := emit(t, h, "web-1", fix.ExecutionID, EmitEventCommand{Name: "info.listo", Message: "ready for review"}); len(answer.Deliveries) != 1 || answer.Deliveries[0].BotID != "a-reviewer" {
		t.Fatalf("listo = %+v", answer)
	}
	if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", fix.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "patched"}}); err != nil {
		t.Fatal(err)
	}
	review := nextFor(t, h, "web-1")
	if review.Run == nil || review.Run.BotID != "a-reviewer" {
		t.Fatalf("review = %+v", review.Run)
	}
	if answer := emit(t, h, "web-1", review.ExecutionID, EmitEventCommand{Name: "order.completed", Message: "merged", Data: json.RawMessage(`{"id": "h-1@4f9c2ab"}`)}); len(answer.Deliveries) != 1 {
		t.Fatalf("completada = %+v", answer)
	}
	back := tasksOf(t, h, "b-triage")
	if len(back) != 1 {
		t.Fatalf("team-b tasks = %d", len(back))
	}
	if origin, _ := back[0].Origin(); origin.Crossings != 2 {
		t.Errorf("crossings = %d, want 2 (the teammate's task counted from 1)", origin.Crossings)
	}
}
