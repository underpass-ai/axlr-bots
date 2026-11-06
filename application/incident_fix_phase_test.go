package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// sreTeam registers a triage bot that takes incidents and the fix bots
// given, all in the sre team; each fix bot declares the fix phase.
func sreTeam(t *testing.T, h *harness, fixers ...string) domain.Bot {
	t.Helper()
	triage := teamBot(t, h, "triage", []string{"sre"}, func(d *BotDefinition) {
		d.Triggers = []domain.TriggerSnapshot{{Type: "manual"}, {Type: "incident"}}
	})
	for _, id := range fixers {
		teamBot(t, h, id, []string{"sre"}, func(d *BotDefinition) {
			d.Triggers = []domain.TriggerSnapshot{{Type: "manual"}, {Type: "incident", Phase: "fix"}}
		})
	}
	h.log.events, h.log.audits = nil, nil
	return triage
}

func completes(answer string, cost float64) ExecutionReport {
	return ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: answer, SessionID: "s"}, Usage: domain.Usage{CostUSD: cost}, TracePath: "/t"}
}

// Diagnosed → Proposed: the control plane launches the fix task when the
// triage task completes, and the fix task's answer is the proposal.
func TestIncidentFixPhaseProposes(t *testing.T) {
	h := newHarness()
	triage := sreTeam(t, h, "fix")
	ctx := context.Background()
	opened, err := NewOpenIncident(h.deps).Execute(ctx, alert(triage, "fp-a"))
	if err != nil {
		t.Fatal(err)
	}
	h.runtime.report = completes("CAUSE: deployment web in shop drifted: replicas 1, desired 3", 0.02)
	if _, err := NewRunTask(h.deps).Execute(ctx, opened.Task.ID()); err != nil {
		t.Fatal(err)
	}
	diagnosed, _ := h.incs.Get(ctx, opened.Incident.ID())
	current, ok := diagnosed.CurrentTask()
	if diagnosed.Status() != domain.IncidentDiagnosed || !ok || current.Phase() != domain.IncidentPhaseFix || current.BotID() != "fix" || len(diagnosed.Tasks()) != 2 {
		t.Fatalf("after triage: %+v", diagnosed.Snapshot())
	}
	fixTask, err := h.tasks.Get(ctx, current.TaskID())
	if err != nil {
		t.Fatal(err)
	}
	id := opened.Incident.ID().String()
	if fixTask.BotID() != "fix" || fixTask.IncidentID() != opened.Incident.ID() || fixTask.IdempotencyKey() != domain.IdempotencyKey("incident:"+id+":1:fix") ||
		fixTask.Trigger().Type != domain.TriggerIncident || fixTask.CorrelationID() != opened.Task.CorrelationID() {
		t.Errorf("fix task = %+v", fixTask.Snapshot())
	}
	objective := fixTask.Objective().String()
	for _, want := range []string{id, "KubePodCrashLooping in shop", "fp-a", "replicas 1, desired 3", "apply nothing"} {
		if !strings.Contains(objective, want) {
			t.Errorf("objective lacks %q: %q", want, objective)
		}
	}
	if !strings.Contains(string(fixTask.Input()), "replicas 1, desired 3") {
		t.Errorf("input = %s", fixTask.Input())
	}
	if last := h.log.audits[len(h.log.audits)-1]; last.Action != "incident.fix" || last.Outcome != domain.AuditOK || last.Details["bot"] != "fix" {
		t.Errorf("audit = %+v", last)
	}
	// The fix task completes: Proposed, with its answer and its cost.
	h.runtime.report = completes("PROPOSAL (not applied): kubectl apply -f desired/web.yaml\nROLLBACK: kubectl scale deploy/web --replicas=1", 0.03)
	if _, err := NewRunTask(h.deps).Execute(ctx, fixTask.ID()); err != nil {
		t.Fatal(err)
	}
	proposed, _ := h.incs.Get(ctx, opened.Incident.ID())
	if proposed.Status() != domain.IncidentProposed || !strings.HasPrefix(proposed.Proposal(), "PROPOSAL (not applied)") || proposed.CostUSD() != 0.05 ||
		proposed.Diagnosis() == "" || proposed.Retries() != 0 {
		t.Fatalf("after fix: %+v", proposed.Snapshot())
	}
	if !h.log.has("incident.proposed", fixTask.ID()) {
		t.Fatalf("events = %v", h.log.eventTypes())
	}
	event := h.log.events[len(h.log.events)-1]
	if event.Type != domain.EventIncidentProposed || event.Payload["proposal"] != proposed.Proposal() || event.Payload["proposedBy"] != "fix" || event.Payload["phase"] != "fix" || event.Payload["status"] != "Proposed" {
		t.Errorf("event = %+v", event)
	}
	// The triage task, no longer the current one, moves nothing.
	stale, _ := h.tasks.Get(ctx, opened.Task.ID())
	before := len(h.log.events)
	if err := newIncidentAdvancer(h.deps).taskEnded(ctx, triage, stale, domain.Execution{}); err != nil || len(h.log.events) != before {
		t.Errorf("a stale triage task moved the incident: %v", err)
	}
	// A repeat while Proposed records the alert and starts nothing.
	tasks := len(h.tasks.tasks)
	again, err := NewOpenIncident(h.deps).Execute(ctx, alert(triage, "fp-a"))
	if err != nil || again.Action != IncidentRepeated || again.Incident.Status() != domain.IncidentProposed || len(h.tasks.tasks) != tasks {
		t.Errorf("repeat while Proposed = %+v %v", again, err)
	}
}

// A fix task that fails, expires or is cancelled escalates the incident:
// the bots could not go on. The diagnosis is kept, and so are the
// retries: a fix that fails every time stops after
// IncidentEscalationRetries.
func TestIncidentFixPhaseFailsAndEscalates(t *testing.T) {
	h := newHarness()
	triage := sreTeam(t, h, "fix")
	ctx := context.Background()
	opened, err := NewOpenIncident(h.deps).Execute(ctx, alert(triage, "fp-a"))
	if err != nil {
		t.Fatal(err)
	}
	for cycle := 1; ; cycle++ {
		h.runtime.report = completes("CAUSE: drift", 0.01)
		incident, _ := h.incs.Get(ctx, opened.Incident.ID())
		if _, err := NewRunTask(h.deps).Execute(ctx, incident.CurrentTaskID()); err != nil {
			t.Fatal(err)
		}
		incident, _ = h.incs.Get(ctx, opened.Incident.ID())
		h.runtime.report = ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "kube-mcp: connection refused", Usage: domain.Usage{CostUSD: 0.01}}
		if _, err := NewRunTask(h.deps).Execute(ctx, incident.CurrentTaskID()); err != nil {
			t.Fatal(err)
		}
		escalated, _ := h.incs.Get(ctx, opened.Incident.ID())
		if escalated.Status() != domain.IncidentEscalated || !strings.HasPrefix(escalated.Reason(), "fix task ") || !strings.Contains(escalated.Reason(), "connection refused") || escalated.Diagnosis() != "CAUSE: drift" {
			t.Fatalf("cycle %d: %+v", cycle, escalated.Snapshot())
		}
		h.clock.now = h.clock.now.Add(4 * time.Minute)
		again, err := NewOpenIncident(h.deps).Execute(ctx, alert(triage, "fp-a"))
		if err != nil {
			t.Fatal(err)
		}
		if again.Action == IncidentRepeated {
			if cycle != domain.IncidentEscalationRetries+1 {
				t.Errorf("stopped retrying after %d cycles", cycle)
			}
			return
		}
		if cycle > domain.IncidentEscalationRetries+1 {
			t.Fatal("a fix that always fails retried for ever")
		}
	}
}

// With two teammates taking the fix phase the control plane does not
// choose; with a paused one it waits. Either way the incident stays
// Diagnosed, audited, and the next alert of the group tries again.
func TestIncidentFixPhaseStaysDiagnosed(t *testing.T) {
	h := newHarness()
	triage := sreTeam(t, h, "fix", "fix-two")
	ctx := context.Background()
	opened, err := NewOpenIncident(h.deps).Execute(ctx, alert(triage, "fp-a"))
	if err != nil {
		t.Fatal(err)
	}
	h.runtime.report = completes("CAUSE: drift", 0.01)
	if _, err := NewRunTask(h.deps).Execute(ctx, opened.Task.ID()); err != nil {
		t.Fatal(err)
	}
	incident, _ := h.incs.Get(ctx, opened.Incident.ID())
	if incident.Status() != domain.IncidentDiagnosed || incident.HasTask(domain.IncidentPhaseFix) {
		t.Fatalf("two fixers: %+v", incident.Snapshot())
	}
	if last := h.log.audits[len(h.log.audits)-1]; last.Action != "incident.fix" || last.Outcome != domain.AuditDenied || !strings.Contains(last.Details["reason"], "fix, fix-two") {
		t.Errorf("audit = %+v", last)
	}
	// One of them leaves the team and the other is paused: still
	// Diagnosed, the refusal audited.
	none := []string{}
	if _, err := NewUpdateBot(h.deps).Execute(ctx, "fix-two", BotPatch{Groups: &none}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPauseBot(h.deps).Execute(ctx, "fix"); err != nil {
		t.Fatal(err)
	}
	h.clock.now = h.clock.now.Add(2 * time.Minute)
	repeated, err := NewOpenIncident(h.deps).Execute(ctx, alert(triage, "fp-a"))
	if err != nil || repeated.Action != IncidentRepeated || repeated.Incident.HasTask(domain.IncidentPhaseFix) {
		t.Fatalf("repeat with the fixer paused = %+v %v", repeated, err)
	}
	if last := h.log.audits[len(h.log.audits)-1]; last.Action != "incident.fix" || last.Outcome != domain.AuditDenied || !strings.Contains(last.Details["reason"], "Paused") {
		t.Errorf("audit = %+v", last)
	}
	// Enabled again: the next alert launches the fix phase.
	if _, err := NewEnableBot(h.deps).Execute(ctx, "fix"); err != nil {
		t.Fatal(err)
	}
	resumed, err := NewOpenIncident(h.deps).Execute(ctx, alert(triage, "fp-a"))
	if err != nil || resumed.Action != IncidentRepeated || !resumed.Incident.HasTask(domain.IncidentPhaseFix) || resumed.Incident.Status() != domain.IncidentDiagnosed {
		t.Fatalf("repeat with the fixer back = %+v %v", resumed, err)
	}
	stored, _ := h.incs.Get(ctx, opened.Incident.ID())
	if stored.CurrentTaskID() != resumed.Incident.CurrentTaskID() {
		t.Error("the fix task was not saved")
	}
	// Once launched, another alert launches nothing more.
	tasks := len(h.tasks.tasks)
	if _, err := NewOpenIncident(h.deps).Execute(ctx, alert(triage, "fp-a")); err != nil || len(h.tasks.tasks) != tasks {
		t.Errorf("a second fix task: %v", err)
	}
}

// A bot that only takes the fix phase triages nothing, and a team
// without one leaves its incidents Diagnosed, as before there was a fix
// phase, with no audit about it.
func TestIncidentFixPhaseNeedsItsTrigger(t *testing.T) {
	h := newHarness()
	triage := sreTeam(t, h)
	fixer := teamBot(t, h, "fix", []string{"other"}, func(d *BotDefinition) {
		d.Triggers = []domain.TriggerSnapshot{{Type: "incident", Phase: "fix"}}
	})
	ctx := context.Background()
	if _, err := NewOpenIncident(h.deps).Execute(ctx, alert(fixer, "fp-a")); err == nil {
		t.Error("a fix bot triaged an incident")
	}
	opened, err := NewOpenIncident(h.deps).Execute(ctx, alert(triage, "fp-a"))
	if err != nil {
		t.Fatal(err)
	}
	h.runtime.report = completes("CAUSE: drift", 0.01)
	if _, err := NewRunTask(h.deps).Execute(ctx, opened.Task.ID()); err != nil {
		t.Fatal(err)
	}
	incident, _ := h.incs.Get(ctx, opened.Incident.ID())
	if incident.Status() != domain.IncidentDiagnosed || len(incident.Tasks()) != 1 {
		t.Fatalf("no teammate fixes: %+v", incident.Snapshot())
	}
	for _, audit := range h.log.audits {
		if audit.Action == "incident.fix" {
			t.Errorf("audit = %+v", audit)
		}
	}
}

// A diagnosis may be twice as long as an objective: the fix objective
// carries what fits and the input the whole of it. A template that does
// not render leaves the incident Diagnosed, audited.
func TestIncidentFixObjectiveFits(t *testing.T) {
	h := newHarness()
	triage := sreTeam(t, h, "fix")
	ctx := context.Background()
	opened, err := NewOpenIncident(h.deps).Execute(ctx, alert(triage, "fp-a"))
	if err != nil {
		t.Fatal(err)
	}
	h.runtime.report = completes(strings.Repeat("evidence ", 1700), 0.01)
	if _, err := NewRunTask(h.deps).Execute(ctx, opened.Task.ID()); err != nil {
		t.Fatal(err)
	}
	incident, _ := h.incs.Get(ctx, opened.Incident.ID())
	fixTask, err := h.tasks.Get(ctx, incident.CurrentTaskID())
	if err != nil || fixTask.BotID() != "fix" {
		t.Fatalf("fix task: %v", err)
	}
	if objective := fixTask.Objective().String(); len(objective) > domain.MaxObjectiveLength || !strings.Contains(objective, "…") || !strings.HasSuffix(objective, "apply nothing.") {
		t.Errorf("objective of %d bytes: %q", len(objective), objective[len(objective)-80:])
	}
	if !strings.Contains(string(fixTask.Input()), strings.TrimSpace(strings.Repeat("evidence ", 1700))) {
		t.Error("the input does not carry the whole diagnosis")
	}
	trigger, _ := domain.NewTrigger(domain.TriggerSnapshot{Type: "incident", Phase: "fix", Objective: "Fix {{.nothing}}"})
	fields, _ := incidentFields(incident)
	if _, err := fixObjective(trigger, incident, fields); err == nil {
		t.Error("a template naming a missing field rendered")
	}
}

// The launch reports what the stores refuse and leaves an incident whose
// triage bot is gone alone.
func TestIncidentFixPhaseLaunchFailures(t *testing.T) {
	h := newHarness()
	triage := sreTeam(t, h, "fix")
	ctx := context.Background()
	diagnosed := func(id domain.IncidentID, bot domain.BotID) domain.Incident {
		t.Helper()
		incident, err := domain.NewIncident(id, triage.TenantID(), bot, "g-"+id.String(), "manual", "drift", domain.Sev3, nil, h.clock.now)
		if err != nil {
			t.Fatal(err)
		}
		_ = incident.AttachTask(domain.TaskID("task-"+id.String()), h.clock.now)
		if err := incident.Diagnose("CAUSE: drift", h.clock.now); err != nil {
			t.Fatal(err)
		}
		return incident
	}
	phase := newFixPhase(h.deps)
	orphan := diagnosed("inc-orphan", "gone")
	if got, err := phase.launch(ctx, orphan, ""); err != nil || got.HasTask(domain.IncidentPhaseFix) {
		t.Errorf("a gone triage bot: %v", err)
	}
	h.bots.listFail = errBoom
	if _, err := phase.launch(ctx, diagnosed("inc-list", triage.ID()), ""); !errors.Is(err, errBoom) {
		t.Errorf("listing the team: %v", err)
	}
	h.bots.listFail = nil
	h.incs.fail = errBoom
	if got, err := phase.launch(ctx, diagnosed("inc-save", triage.ID()), ""); !errors.Is(err, errBoom) || got.HasTask(domain.IncidentPhaseFix) {
		t.Errorf("saving the incident: %v", err)
	}
	h.incs.fail = nil
	h.tasks.fail = errBoom
	if _, err := phase.launch(ctx, diagnosed("inc-submit", triage.ID()), ""); !errors.Is(err, errBoom) {
		t.Errorf("submitting the task: %v", err)
	}
}
