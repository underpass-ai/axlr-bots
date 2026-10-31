package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// triageBot is an enabled bot that takes incidents, with the objective
// template given (the default when empty).
func triageBot(t *testing.T, h *harness, name, template string) domain.Bot {
	t.Helper()
	ctx := context.Background()
	definition := sampleDefinition()
	definition.Name = name
	definition.Triggers = []domain.TriggerSnapshot{{Type: "manual"}, {Type: "incident", Objective: template}}
	bot, err := NewRegisterBot(h.deps).Execute(ctx, definition)
	if err != nil {
		t.Fatal(err)
	}
	if bot, err = NewEnableBot(h.deps).Execute(ctx, bot.ID()); err != nil {
		t.Fatal(err)
	}
	h.log.events, h.log.audits = nil, nil
	return bot
}

func alert(bot domain.Bot, fingerprints ...string) OpenIncidentCommand {
	return OpenIncidentCommand{Tenant: bot.TenantID().String(), BotID: bot.ID().String(), GroupKey: `{}/{alertname="KubePodCrashLooping"}`, Source: "alertmanager", Title: "KubePodCrashLooping in shop", Severity: "sev2", Fingerprints: fingerprints}
}

func TestOpenIncidentOpensOnceAndRepeats(t *testing.T) {
	h := newHarness()
	bot := triageBot(t, h, "triage", "")
	ctx := context.Background()
	uc := NewOpenIncident(h.deps)
	first, err := uc.Execute(ctx, alert(bot, "fp-a"))
	if err != nil || !first.Created || first.Reopened || first.Task == nil || first.Incident.Status() != domain.IncidentOpen || first.Incident.Cycle() != 1 {
		t.Fatalf("first = %+v %v", first, err)
	}
	task := *first.Task
	if task.IncidentID() != first.Incident.ID() || first.Incident.CurrentTaskID() != task.ID() || task.Trigger().Type != domain.TriggerIncident || task.IdempotencyKey() != domain.IdempotencyKey("incident:"+first.Incident.ID().String()+":1") {
		t.Errorf("task = %+v", task.Snapshot())
	}
	if objective := task.Objective().String(); !strings.Contains(objective, "KubePodCrashLooping in shop") || !strings.Contains(objective, "sev2") || !strings.Contains(objective, "fp-a") {
		t.Errorf("objective = %q", objective)
	}
	if string(task.Input()) == "" || !strings.Contains(string(task.Input()), `"groupKey"`) {
		t.Errorf("input = %s", task.Input())
	}
	if got := h.log.eventTypes(); len(got) != 2 || got[0] != "bot.task.submitted" || got[1] != "incident.opened" {
		t.Fatalf("events = %v", got)
	}
	if h.log.events[1].IncidentID != first.Incident.ID() || h.log.events[1].TaskID != task.ID() || h.log.events[0].Payload["incidentId"] != first.Incident.ID().String() {
		t.Errorf("event = %+v", h.log.events[1])
	}
	stored, _ := h.incs.Get(ctx, first.Incident.ID())
	if stored.CurrentTaskID() != task.ID() {
		t.Error("the task was not attached in the store")
	}
	// The same group again while open: a repeat, nothing started.
	h.clock.now = t0.Add(2 * time.Minute)
	again, err := uc.Execute(ctx, alert(bot, "fp-a", "fp-b"))
	if err != nil || again.Created || again.Reopened || again.Task != nil || again.Incident.ID() != first.Incident.ID() || again.Incident.Repeats() != 1 || len(again.Incident.Fingerprints()) != 2 {
		t.Fatalf("repeat = %+v %v", again, err)
	}
	if len(h.tasks.tasks) != 1 {
		t.Errorf("tasks = %d, want 1", len(h.tasks.tasks))
	}
	if got := h.log.eventTypes(); got[len(got)-1] != "incident.repeated" || h.log.events[len(got)-1].Payload["repeats"] != 1 {
		t.Errorf("events = %v", got)
	}
	if last := h.log.audits[len(h.log.audits)-1]; last.Details["deduplicated"] != "true" {
		t.Errorf("audit = %+v", last)
	}
	// Another group opens another incident.
	other := alert(bot, "fp-z")
	other.GroupKey = `{}/{alertname="KubePodNotReady"}`
	second, err := uc.Execute(ctx, other)
	if err != nil || !second.Created || second.Incident.ID() == first.Incident.ID() {
		t.Fatalf("second group = %+v %v", second, err)
	}
	list, err := NewListIncidents(h.deps).Execute(ctx, IncidentQuery{})
	if err != nil || len(list) != 2 || list[0].ID() != second.Incident.ID() {
		t.Errorf("list = %d %v", len(list), err)
	}
	open, _ := NewListIncidents(h.deps).Execute(ctx, IncidentQuery{Status: "Open", Tenant: bot.TenantID().String(), Limit: 1})
	if len(open) != 1 {
		t.Errorf("open = %d", len(open))
	}
	for _, query := range []IncidentQuery{{Status: "Closed"}, {Tenant: "Bad Tenant"}, {Limit: -1}} {
		if _, err := NewListIncidents(h.deps).Execute(ctx, query); err == nil {
			t.Errorf("query %+v accepted", query)
		}
	}
	if got, err := NewGetIncident(h.deps).Execute(ctx, first.Incident.ID()); err != nil || got.ID() != first.Incident.ID() {
		t.Errorf("get: %v", err)
	}
}

func TestOpenIncidentRefusals(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	uc := NewOpenIncident(h.deps)
	bot := enabledBot(t, h) // no incident trigger
	var serr domain.StateError
	if _, err := uc.Execute(ctx, alert(bot, "fp")); !errors.As(err, &serr) {
		t.Errorf("a bot without the trigger took an incident: %v", err)
	}
	if len(h.incs.incidents) != 0 || len(h.tasks.tasks) != 0 {
		t.Error("something was saved for a refused incident")
	}
	triage := triageBot(t, h, "triage", "")
	// A paused bot refuses the open and nothing is saved.
	if _, err := NewPauseBot(h.deps).Execute(ctx, triage.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Execute(ctx, alert(triage, "fp")); !errors.As(err, &serr) {
		t.Errorf("a paused bot took an incident: %v", err)
	}
	if len(h.incs.incidents) != 0 || len(h.tasks.tasks) != 0 {
		t.Error("something was saved for a paused bot")
	}
	if _, err := NewEnableBot(h.deps).Execute(ctx, triage.ID()); err != nil {
		t.Fatal(err)
	}
	bad := []OpenIncidentCommand{
		{Tenant: "", BotID: "triage", GroupKey: "g", Title: "t"},
		{Tenant: "engineering", BotID: "", GroupKey: "g", Title: "t"},
		{Tenant: "engineering", BotID: "triage", GroupKey: "g", Title: "t", Severity: "high"},
		{Tenant: "engineering", BotID: "triage", GroupKey: "", Title: "t"},
		{Tenant: "engineering", BotID: "triage", GroupKey: "g", Title: ""},
		{Tenant: "engineering", BotID: "triage", GroupKey: "g", Title: "t", Source: "Alert Manager"},
	}
	for n, command := range bad {
		if _, err := uc.Execute(ctx, command); err == nil {
			t.Errorf("bad command %d accepted", n)
		}
	}
	if _, err := uc.Execute(ctx, OpenIncidentCommand{Tenant: "other", BotID: triage.ID().String(), GroupKey: "g", Title: "t"}); !domain.IsNotFound(err) {
		t.Errorf("a bot of another tenant: %v", err)
	}
	if _, err := uc.Execute(ctx, OpenIncidentCommand{Tenant: "engineering", BotID: "ghost", GroupKey: "g", Title: "t"}); !domain.IsNotFound(err) {
		t.Errorf("unknown bot: %v", err)
	}
	// A template that names a field the incident lacks refuses the open.
	picky := triageBot(t, h, "picky", "Look at {{.cluster}}")
	if _, err := uc.Execute(ctx, OpenIncidentCommand{Tenant: "engineering", BotID: picky.ID().String(), GroupKey: "g", Title: "t"}); err == nil {
		t.Error("a template over a missing field rendered")
	}
	// The alert's own objective wins over the template.
	command := OpenIncidentCommand{Tenant: "engineering", BotID: picky.ID().String(), GroupKey: "g2", Title: "t", Objective: "Just look"}
	if result, err := uc.Execute(ctx, command); err != nil || result.Task.Objective() != "Just look" {
		t.Errorf("own objective: %v", err)
	}
	// A failing store is reported.
	h.incs.listFail = errBoom
	if _, err := uc.Execute(ctx, alert(triage, "fp")); !errors.Is(err, errBoom) {
		t.Errorf("store failure: %v", err)
	}
	h.incs.listFail = nil
	h.incs.fail = errBoom
	if _, err := uc.Execute(ctx, alert(triage, "fp")); !errors.Is(err, errBoom) {
		t.Errorf("save failure: %v", err)
	}
}

func TestIncidentAdvancesWithItsTriageTask(t *testing.T) {
	h := newHarness()
	bot := triageBot(t, h, "triage", "")
	ctx := context.Background()
	h.runtime.report = ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "OOMKilled: limit 32Mi, usage 60Mi", SessionID: "s"}, Usage: domain.Usage{CostUSD: 0.02}, TracePath: "/t"}
	opened, err := NewOpenIncident(h.deps).Execute(ctx, alert(bot, "fp-a"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRunTask(h.deps).Execute(ctx, opened.Task.ID()); err != nil {
		t.Fatal(err)
	}
	incident, _ := h.incs.Get(ctx, opened.Incident.ID())
	if incident.Status() != domain.IncidentDiagnosed || incident.Diagnosis() != "OOMKilled: limit 32Mi, usage 60Mi" || incident.CostUSD() != 0.02 {
		t.Fatalf("after the run: %+v", incident.Snapshot())
	}
	if !h.log.has("incident.diagnosed", opened.Task.ID()) {
		t.Errorf("events = %v", h.log.eventTypes())
	}
	// Resolving by hand, then an alert inside the window reopens with a
	// new cycle and a new task; the diagnosis stays until a new one.
	h.clock.now = t0.Add(5 * time.Minute)
	resolved, err := NewResolveIncident(h.deps).Execute(ctx, incident.ID(), "fixed by hand")
	if err != nil || resolved.Status() != domain.IncidentResolved || resolved.Reason() != "fixed by hand" {
		t.Fatalf("resolve: %v", err)
	}
	if _, err := NewResolveIncident(h.deps).Execute(ctx, incident.ID(), ""); err == nil {
		t.Error("resolved twice")
	}
	if _, err := NewResolveIncident(h.deps).Execute(ctx, "inc-ghost", ""); !domain.IsNotFound(err) {
		t.Errorf("unknown incident: %v", err)
	}
	h.clock.now = t0.Add(10 * time.Minute)
	reopened, err := NewOpenIncident(h.deps).Execute(ctx, alert(bot, "fp-b"))
	if err != nil || !reopened.Reopened || reopened.Created || reopened.Incident.ID() != incident.ID() || reopened.Incident.Cycle() != 2 || reopened.Task == nil || reopened.Task.ID() == opened.Task.ID() {
		t.Fatalf("reopen = %+v %v", reopened, err)
	}
	if reopened.Incident.Diagnosis() == "" || reopened.Incident.CurrentTaskID() != reopened.Task.ID() || len(reopened.Incident.TaskIDs()) != 2 {
		t.Errorf("reopened = %+v", reopened.Incident.Snapshot())
	}
	if got := h.log.eventTypes(); got[len(got)-1] != "incident.reopened" {
		t.Errorf("events = %v", got)
	}
	// The stale task of cycle 1 cannot move the incident; cycle 2's can.
	stale, _ := h.tasks.Get(ctx, opened.Task.ID())
	before := len(h.log.events)
	if err := newIncidentAdvancer(h.deps).taskEnded(ctx, bot, stale, domain.Execution{}); err != nil {
		t.Fatal(err)
	}
	current, _ := h.incs.Get(ctx, incident.ID())
	if current.Status() != domain.IncidentOpen || len(h.log.events) != before {
		t.Error("a stale task moved the incident")
	}
	if last := h.log.audits[len(h.log.audits)-1]; last.Outcome != domain.AuditDenied || !strings.Contains(last.Details["reason"], "current cycle") {
		t.Errorf("audit = %+v", last)
	}
	// This time the triage task fails: the incident is escalated.
	h.runtime.report = ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "kubectl: connection refused", Usage: domain.Usage{CostUSD: 0.01}}
	if _, err := NewRunTask(h.deps).Execute(ctx, reopened.Task.ID()); err != nil {
		t.Fatal(err)
	}
	escalated, _ := h.incs.Get(ctx, incident.ID())
	if escalated.Status() != domain.IncidentEscalated || !strings.Contains(escalated.Reason(), "connection refused") || escalated.CostUSD() != 0.03 {
		t.Fatalf("after the failure: %+v", escalated.Snapshot())
	}
	if !h.log.has("incident.escalated", reopened.Task.ID()) {
		t.Errorf("events = %v", h.log.eventTypes())
	}
	// Outside the window a new alert opens another incident.
	_, _ = NewResolveIncident(h.deps).Execute(ctx, incident.ID(), "")
	h.clock.now = h.clock.now.Add(domain.IncidentReopenWindow)
	another, err := NewOpenIncident(h.deps).Execute(ctx, alert(bot, "fp-c"))
	if err != nil || !another.Created || another.Incident.ID() == incident.ID() {
		t.Fatalf("after the window = %+v %v", another, err)
	}
}

func TestIncidentAdvancerLeavesAGoneOrResolvedIncident(t *testing.T) {
	h := newHarness()
	bot := triageBot(t, h, "triage", "")
	ctx := context.Background()
	opened, err := NewOpenIncident(h.deps).Execute(ctx, alert(bot, "fp-a"))
	if err != nil {
		t.Fatal(err)
	}
	// Resolved by the source before the triage task ends: the task
	// settles, the cost is kept, the status stays.
	if _, err := NewResolveIncident(h.deps).Execute(ctx, opened.Incident.ID(), "alerts green"); err != nil {
		t.Fatal(err)
	}
	h.runtime.report.Usage = domain.Usage{CostUSD: 0.05}
	if _, err := NewRunTask(h.deps).Execute(ctx, opened.Task.ID()); err != nil {
		t.Fatal(err)
	}
	task, _ := h.tasks.Get(ctx, opened.Task.ID())
	incident, _ := h.incs.Get(ctx, opened.Incident.ID())
	if task.Status() != domain.TaskCompleted || incident.Status() != domain.IncidentResolved || incident.CostUSD() != 0.05 || incident.Diagnosis() != "" {
		t.Errorf("task %s, incident %+v", task.Status(), incident.Snapshot())
	}
	if last := h.log.audits[len(h.log.audits)-1]; last.Action != "incident.advance" || last.Outcome != domain.AuditDenied {
		t.Errorf("audit = %+v", last)
	}
	// A cancelled triage task escalates its incident.
	second, _ := NewOpenIncident(h.deps).Execute(ctx, func() OpenIncidentCommand { c := alert(bot, "fp-b"); c.GroupKey = "g2"; return c }())
	if _, err := NewCancelTask(h.deps).Execute(ctx, second.Task.ID()); err != nil {
		t.Fatal(err)
	}
	if cancelled, _ := h.incs.Get(ctx, second.Incident.ID()); cancelled.Status() != domain.IncidentEscalated || !strings.Contains(cancelled.Reason(), "Cancelled") {
		t.Errorf("after cancel: %+v", cancelled.Snapshot())
	}
	// An incident that is gone does not stop the task from settling.
	third, _ := NewOpenIncident(h.deps).Execute(ctx, func() OpenIncidentCommand { c := alert(bot, "fp-c"); c.GroupKey = "g3"; return c }())
	delete(h.incs.incidents, third.Incident.ID())
	if _, err := NewRunTask(h.deps).Execute(ctx, third.Task.ID()); err != nil {
		t.Fatal(err)
	}
	if task, _ := h.tasks.Get(ctx, third.Task.ID()); task.Status() != domain.TaskCompleted {
		t.Errorf("task = %s", task.Status())
	}
	// An incident saved without its task (a crash between the two saves)
	// gets it from the next alert of the group, in its own cycle.
	orphan, _ := domain.NewIncident("inc-orphan", bot.TenantID(), bot.ID(), "g-orphan", "alertmanager", "orphan", domain.Sev3, nil, h.clock.now)
	_ = h.incs.Save(ctx, orphan)
	resumed, err := NewOpenIncident(h.deps).Execute(ctx, func() OpenIncidentCommand { c := alert(bot, "fp-o"); c.GroupKey = "g-orphan"; return c }())
	if err != nil || resumed.Task == nil || resumed.Incident.ID() != "inc-orphan" || resumed.Incident.CurrentTaskID() != resumed.Task.ID() || !resumed.Created || resumed.Incident.Repeats() != 1 {
		t.Errorf("orphan = %+v %v", resumed, err)
	}
	// A task of no incident is left alone; a store failure is reported.
	plain := submit(t, h, bot, "k")
	if err := newIncidentAdvancer(h.deps).taskEnded(ctx, bot, plain.Task, domain.Execution{}); err != nil {
		t.Error(err)
	}
	fourth, _ := NewOpenIncident(h.deps).Execute(ctx, func() OpenIncidentCommand { c := alert(bot, "fp-d"); c.GroupKey = "g4"; return c }())
	h.incs.fail = errBoom
	if _, err := NewRunTask(h.deps).Execute(ctx, fourth.Task.ID()); !errors.Is(err, errBoom) {
		t.Errorf("store failure: %v", err)
	}
}
