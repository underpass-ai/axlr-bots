package application

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

// postmortemTeam is the brigade with a scribe: the postmortem's trigger
// takes the severities given (the default when none is).
func postmortemTeam(t *testing.T, h *harness, approvers []string, severities ...string) domain.Bot {
	t.Helper()
	triage := brigade(t, h, domain.ApprovalsSnapshot{})
	teamBot(t, h, "scribe", []string{"sre"}, func(d *BotDefinition) {
		d.Approvals = domain.ApprovalsSnapshot{Approvers: approvers}
		d.Triggers = []domain.TriggerSnapshot{{Type: "incident", Phase: "postmortem", Severities: severities}}
	})
	h.log.events, h.log.audits = nil, nil
	return triage
}

// resolvedIncident opens an incident, triages it and closes it by hand.
func resolvedIncidentOf(t *testing.T, h *harness, triage domain.Bot, severity string) domain.Incident {
	t.Helper()
	ctx := context.Background()
	command := alert(triage, "fp-a")
	command.Severity = severity
	opened, err := NewOpenIncident(h.deps).Execute(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	h.runtime.report = completes("CAUSE: replicas 1, desired 3", 0.01)
	if _, err := NewRunTask(h.deps).Execute(ctx, opened.Incident.CurrentTaskID()); err != nil {
		t.Fatal(err)
	}
	incident, err := NewResolveIncident(h.deps).Execute(ctx, opened.Incident.ID(), "alerts green")
	if err != nil {
		t.Fatal(err)
	}
	return incident
}

// draftOf runs the postmortem's task to its answer.
func draftOf(t *testing.T, h *harness, incident domain.Incident, answer string) domain.Incident {
	t.Helper()
	ctx := context.Background()
	h.runtime.report = completes(answer, 0.02)
	if _, err := NewRunTask(h.deps).Execute(ctx, incident.CurrentTaskID()); err != nil {
		t.Fatal(err)
	}
	moved, err := h.incs.Get(ctx, incident.ID())
	if err != nil {
		t.Fatal(err)
	}
	return moved
}

func postmortemState(t *testing.T, incident domain.Incident) domain.IncidentPostmortemState {
	t.Helper()
	postmortem, ok := incident.Postmortem()
	if !ok {
		t.Fatalf("no postmortem: %+v", incident.Snapshot())
	}
	return postmortem.State()
}

func auditNamed(t *testing.T, h *harness, action string) domain.AuditEntry {
	t.Helper()
	audits := auditsOf(h, action)
	if len(audits) == 0 {
		t.Fatalf("no audit %s: %+v", action, h.log.audits)
	}
	return audits[len(audits)-1]
}

// The whole phase on a brigade that applied and verified: the input is
// the control plane's facts and no secret, the answer is the draft, a
// named person approves it and the outputs announce it.
func TestAResolvedIncidentGetsItsPostmortemApprovedAndAnnounced(t *testing.T) {
	h := newHarness()
	triage := brigade(t, h, domain.ApprovalsSnapshot{})
	const secret = "s3cret-trigger-credential-value"
	teamBot(t, h, "scribe", []string{"sre"}, func(d *BotDefinition) {
		d.Approvals = domain.ApprovalsSnapshot{Approvers: []string{"tirso"}}
		d.Triggers = []domain.TriggerSnapshot{{Type: "incident", Phase: "postmortem"}, {Type: "webhook", Name: "hook", Secret: secret, Objective: "x"}}
	})
	ctx := context.Background()
	if _, err := NewAddOutput(h.deps).Execute(ctx, OutputDefinition{Name: "chat", Type: "slack", URL: "https://hooks.example.com/" + secret}); err != nil {
		t.Fatal(err)
	}
	incident := proposed(t, h, triage)
	apply := waitingApply(t, h, incident)
	h.runtime.report = completes("applied", 0.01)
	h.decider.report = completes("applied", 0.01)
	if _, err := NewApproveTask(h.deps).Execute(ctx, apply.ID(), DecisionApprove, "", "tirso"); err != nil {
		t.Fatal(err)
	}
	if moved, _ := h.incs.Get(ctx, incident.ID()); moved.Status() != domain.IncidentVerifying {
		t.Fatalf("status %s", moved.Status())
	}
	moved, _ := h.incs.Get(ctx, incident.ID())
	h.runtime.report = completes("VERDICT: green", 0.01)
	if _, err := NewRunTask(h.deps).Execute(ctx, moved.CurrentTaskID()); err != nil {
		t.Fatal(err)
	}
	h.log.events = append(h.log.events, domain.Event{ID: "evt-noise", SchemaVersion: domain.EventSchemaV1, Type: domain.EventTaskWaitingApproval, TenantID: "default", BotID: "fix", TaskID: apply.ID(), CreatedAt: t0, Payload: map[string]any{"tool": "local_write", "arguments": secret, "callId": "c-other"}})

	resolved, err := NewResolveIncident(h.deps).Execute(ctx, incident.ID(), "alerts green")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status() != domain.IncidentResolved || postmortemState(t, resolved) != domain.PostmortemWriting {
		t.Fatalf("resolved: %+v", resolved.Snapshot())
	}
	task, err := h.tasks.Get(ctx, resolved.CurrentTaskID())
	if err != nil {
		t.Fatal(err)
	}
	if task.BotID() != "scribe" || task.IncidentID() != incident.ID() || task.IdempotencyKey() != domain.IdempotencyKey("incident:"+incident.ID().String()+":1:postmortem") {
		t.Errorf("task = %+v", task.Snapshot())
	}
	raw := string(task.Input())
	if strings.Contains(raw, secret) || strings.Contains(task.Objective().String(), secret) || strings.Contains(raw, "arguments") {
		t.Fatalf("a secret in the input: %s", raw)
	}
	var input PostmortemInput
	if err := json.Unmarshal(task.Input(), &input); err != nil {
		t.Fatal(err)
	}
	if input.Schema != PostmortemInputSchema || !strings.Contains(input.Diagnosis, "replicas 1, desired 3") || !strings.Contains(input.Proposal, "kubectl apply") || input.CostUSD <= 0 || input.ResolvedAt.IsZero() {
		t.Errorf("input = %+v", input)
	}
	if len(input.Verdicts) != 1 || input.Verdicts[0].Verdict != "green" || input.Verdicts[0].Cycle != 1 {
		t.Errorf("verdicts = %+v", input.Verdicts)
	}
	var approved bool
	for _, decision := range input.Decisions {
		approved = approved || (decision.By == "tirso" && decision.Outcome == domain.ApprovalApproved && decision.Phase == "apply" && decision.Tool == "local_write" && !decision.At.IsZero())
	}
	if !approved {
		t.Errorf("decisions = %+v", input.Decisions)
	}
	var diagnosed, decided bool
	for _, entry := range input.Timeline {
		diagnosed = diagnosed || entry.Event == domain.EventIncidentDiagnosed.String()
		decided = decided || (entry.Event == domain.EventTaskDecided.String() && entry.Actor == "tirso" && entry.Phase == "apply")
	}
	if !diagnosed || !decided {
		t.Errorf("timeline = %+v", input.Timeline)
	}
	objective := task.Objective().String()
	for _, want := range []string{"replicas 1, desired 3", "kubectl apply", "cycle 1: green", "approved by tirso", "incident.diagnosed", "blameless"} {
		if !strings.Contains(objective, want) {
			t.Errorf("the objective misses %q: %s", want, objective)
		}
	}

	drafted := draftOf(t, h, resolved, "# Postmortem\n\nThe replicas drifted.")
	if drafted.Status() != domain.IncidentResolved || postmortemState(t, drafted) != domain.PostmortemDraft {
		t.Fatalf("drafted: %+v", drafted.Snapshot())
	}
	if postmortem, _ := drafted.Postmortem(); postmortem.AuthorTaskID() != task.ID() || postmortem.Markdown() != "# Postmortem\n\nThe replicas drifted." {
		t.Errorf("draft: %+v", postmortem.Snapshot())
	}
	if _, ok := lastOf(h, domain.EventIncidentPostmortemDrafted); !ok {
		t.Error("no drafted event")
	}
	before := len(h.outbox2.all())
	final, err := NewReviewPostmortem(h.deps).Approve(ctx, incident.ID(), "tirso")
	if err != nil {
		t.Fatal(err)
	}
	if postmortemState(t, final) != domain.PostmortemApproved {
		t.Fatalf("approved: %+v", final.Snapshot())
	}
	event, ok := lastOf(h, domain.EventIncidentPostmortemApproved)
	if !ok || event.Payload["approvedBy"] != "tirso" {
		t.Errorf("event = %+v", event)
	}
	audit := auditNamed(t, h, "incident.postmortem.approve")
	if audit.Actor != "tirso" || audit.Outcome != domain.AuditOK || audit.Details["authorTask"] != task.ID().String() {
		t.Errorf("audit = %+v", audit)
	}
	deliveries := h.outbox2.all()
	if len(deliveries) != before+1 {
		t.Fatalf("deliveries %d, want %d", len(deliveries), before+1)
	}
	last := deliveries[len(deliveries)-1]
	if last.Event() != domain.EventIncidentPostmortemApproved || !strings.Contains(string(last.Payload()), "The replicas drifted") || !strings.Contains(string(last.Payload()), "Postmortem approved") || strings.Contains(string(last.Payload()), secret) {
		t.Errorf("delivery %s: %s", last.Event(), last.Payload())
	}
}

func TestOnlyTheSeveritiesOfTheTriggerGetAPostmortem(t *testing.T) {
	h := newHarness()
	triage := postmortemTeam(t, h, nil)
	incident := resolvedIncidentOf(t, h, triage, "sev3")
	if _, ok := incident.Postmortem(); ok {
		t.Fatalf("a sev3 got one by default: %+v", incident.Snapshot())
	}
	if audit := auditNamed(t, h, postmortemAction); audit.Outcome != domain.AuditOK || !strings.Contains(audit.Details["skipped"], "sev3") {
		t.Errorf("audit = %+v", audit)
	}

	lab := newHarness()
	triage = postmortemTeam(t, lab, nil, "sev1", "sev3")
	if incident := resolvedIncidentOf(t, lab, triage, "sev3"); postmortemState(t, incident) != domain.PostmortemWriting {
		t.Errorf("the lab's sev3: %+v", incident.Snapshot())
	}
}

func TestNoneOrTwoTakingThePhaseLeaveItResolvedAndAudited(t *testing.T) {
	h := newHarness()
	triage := brigade(t, h, domain.ApprovalsSnapshot{})
	incident := resolvedIncidentOf(t, h, triage, "sev1")
	if _, ok := incident.Postmortem(); ok || incident.Status() != domain.IncidentResolved {
		t.Fatalf("no taker: %+v", incident.Snapshot())
	}
	if audit := auditNamed(t, h, postmortemAction); audit.Outcome != domain.AuditDenied || !strings.Contains(audit.Details["reason"], "no teammate") {
		t.Errorf("audit = %+v", audit)
	}

	h = newHarness()
	triage = postmortemTeam(t, h, nil)
	teamBot(t, h, "scribe-two", []string{"sre"}, func(d *BotDefinition) {
		d.Triggers = []domain.TriggerSnapshot{{Type: "incident", Phase: "postmortem"}}
	})
	incident = resolvedIncidentOf(t, h, triage, "sev1")
	if _, ok := incident.Postmortem(); ok || incident.Status() != domain.IncidentResolved {
		t.Fatalf("two takers: %+v", incident.Snapshot())
	}
	if audit := auditNamed(t, h, postmortemAction); audit.Outcome != domain.AuditDenied || !strings.Contains(audit.Details["reason"], "scribe, scribe-two") {
		t.Errorf("audit = %+v", audit)
	}
}

// The scribe's failure, an empty answer and a bot that refuses the task
// never move the incident.
func TestAPostmortemThatFailsLeavesTheIncidentResolved(t *testing.T) {
	ctx := context.Background()
	for name, report := range map[string]ExecutionReport{
		"failed": {Outcome: domain.ExecutionFailed, FailureReason: "the model was down"},
		"empty":  completes("  ", 0),
	} {
		h := newHarness()
		triage := postmortemTeam(t, h, nil)
		incident := resolvedIncidentOf(t, h, triage, "sev1")
		h.runtime.report = report
		if _, err := NewRunTask(h.deps).Execute(ctx, incident.CurrentTaskID()); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		moved, _ := h.incs.Get(ctx, incident.ID())
		if moved.Status() != domain.IncidentResolved || postmortemState(t, moved) != domain.PostmortemFailed || moved.SendBacks() != 0 {
			t.Fatalf("%s: %+v", name, moved.Snapshot())
		}
		if _, ok := lastOf(h, domain.EventIncidentEscalated); ok {
			t.Errorf("%s: escalated", name)
		}
		if _, ok := lastOf(h, domain.EventIncidentPostmortemFailed); !ok {
			t.Errorf("%s: no failed event", name)
		}
	}

	h := newHarness()
	triage := postmortemTeam(t, h, nil)
	if _, err := NewPauseBot(h.deps).Execute(ctx, "scribe"); err != nil {
		t.Fatal(err)
	}
	incident := resolvedIncidentOf(t, h, triage, "sev1")
	if _, ok := incident.Postmortem(); ok || incident.Status() != domain.IncidentResolved {
		t.Fatalf("paused: %+v", incident.Snapshot())
	}
	if audit := auditNamed(t, h, postmortemAction); audit.Outcome != domain.AuditDenied || !strings.Contains(audit.Details["reason"], "Paused") {
		t.Errorf("audit = %+v", audit)
	}

	// A store that fails while it launches is audited; the resolve holds.
	h = newHarness()
	triage = postmortemTeam(t, h, nil)
	opened, err := NewOpenIncident(h.deps).Execute(ctx, alert(triage, "fp-a"))
	if err != nil {
		t.Fatal(err)
	}
	h.bots.listFail = errBoom
	resolved, err := NewResolveIncident(h.deps).Execute(ctx, opened.Incident.ID(), "")
	h.bots.listFail = nil
	if err != nil || resolved.Status() != domain.IncidentResolved {
		t.Fatalf("store fails: %v %+v", err, resolved.Snapshot())
	}
	if audit := auditNamed(t, h, postmortemAction); audit.Outcome != domain.AuditFailed {
		t.Errorf("audit = %+v", audit)
	}
}

func TestSendingBackRelaunchesWithTheNoteThenLeavesItToAPerson(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	triage := postmortemTeam(t, h, nil)
	incident := resolvedIncidentOf(t, h, triage, "sev2")
	first := incident.CurrentTaskID()
	draftOf(t, h, incident, "draft one")
	review := NewReviewPostmortem(h.deps)
	if _, err := review.SendBack(ctx, incident.ID(), "tirso", "  "); err == nil {
		t.Fatal("a send-back without a note")
	}
	back, err := review.SendBack(ctx, incident.ID(), "tirso", "name the owner of each action")
	if err != nil {
		t.Fatal(err)
	}
	postmortem, _ := back.Postmortem()
	if postmortem.State() != domain.PostmortemWriting || postmortem.TaskID() == first || postmortem.SendBacks() != 1 || back.Status() != domain.IncidentResolved {
		t.Fatalf("back: %+v", back.Snapshot())
	}
	task, err := h.tasks.Get(ctx, postmortem.TaskID())
	if err != nil {
		t.Fatal(err)
	}
	if task.IdempotencyKey() != domain.IdempotencyKey("incident:"+incident.ID().String()+":1:postmortem:2") {
		t.Errorf("key = %s", task.IdempotencyKey())
	}
	if objective := task.Objective().String(); !strings.Contains(objective, "name the owner of each action") || !strings.Contains(objective, "draft one") {
		t.Errorf("objective = %s", objective)
	}
	var input PostmortemInput
	_ = json.Unmarshal(task.Input(), &input)
	if input.Review == nil || input.Review.Attempt != 2 || input.Review.By != "tirso" || input.Review.Draft != "draft one" {
		t.Errorf("review = %+v", input.Review)
	}
	if event, ok := lastOf(h, domain.EventIncidentPostmortemSentBack); !ok || event.Payload["by"] != "tirso" {
		t.Errorf("event = %+v", event)
	}
	if audit := auditNamed(t, h, "incident.postmortem.send_back"); audit.Actor != "tirso" {
		t.Errorf("audit = %+v", audit)
	}
	tasks := len(h.tasks.tasks)
	draftOf(t, h, back, "draft two")
	left, err := review.SendBack(ctx, incident.ID(), "tirso", "still wrong")
	if err != nil {
		t.Fatal(err)
	}
	if postmortemState(t, left) != domain.PostmortemLeftToPerson || left.Status() != domain.IncidentResolved || len(h.tasks.tasks) != tasks {
		t.Fatalf("left: %+v", left.Snapshot())
	}
	if _, err := review.SendBack(ctx, incident.ID(), "tirso", "again"); !isState(err) {
		t.Errorf("past the cap: %v", err)
	}
	if audit := auditNamed(t, h, "incident.postmortem.refused"); audit.Actor != "tirso" || audit.Details["decision"] != DecisionSendBack {
		t.Errorf("audit = %+v", audit)
	}
	final, err := review.Approve(ctx, incident.ID(), "tirso")
	if err != nil || postmortemState(t, final) != domain.PostmortemApproved {
		t.Fatalf("the person took the last draft: %v", err)
	}
	if postmortem, _ := final.Postmortem(); postmortem.Markdown() != "draft two" {
		t.Errorf("text = %q", postmortem.Markdown())
	}
}

// A send-back whose launch cannot start stays SentBack, and another
// send-back starts it without counting twice.
func TestASendBackWhoseTaskCannotStartIsTriedAgain(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	triage := postmortemTeam(t, h, nil)
	incident := resolvedIncidentOf(t, h, triage, "sev1")
	draftOf(t, h, incident, "draft")
	if _, err := NewPauseBot(h.deps).Execute(ctx, "scribe"); err != nil {
		t.Fatal(err)
	}
	review := NewReviewPostmortem(h.deps)
	stuck, err := review.SendBack(ctx, incident.ID(), "tirso", "more")
	if err != nil || postmortemState(t, stuck) != domain.PostmortemSentBack {
		t.Fatalf("%v %+v", err, stuck.Snapshot())
	}
	if _, err := NewEnableBot(h.deps).Execute(ctx, "scribe"); err != nil {
		t.Fatal(err)
	}
	again, err := review.SendBack(ctx, incident.ID(), "oncall", "more, please")
	if err != nil || postmortemState(t, again) != domain.PostmortemWriting {
		t.Fatalf("%v %+v", err, again.Snapshot())
	}
	if postmortem, _ := again.Postmortem(); postmortem.SendBacks() != 1 || postmortem.Note() != "more, please" {
		t.Errorf("%+v", postmortem.Snapshot())
	}
}

func TestWhoMayReviewAPostmortem(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	triage := postmortemTeam(t, h, []string{"tirso", "oncall"})
	incident := resolvedIncidentOf(t, h, triage, "sev1")
	draftOf(t, h, incident, "draft")
	daemon := h.deps
	daemon.Daemon, daemon.Actor = true, "axlr-daemon"
	review := NewReviewPostmortem(daemon)
	for name, actor := range map[string]string{"nobody": "", "a policy": "policy:autoApprove", "the daemon": "cli:AXLR-daemon", "not listed": "mallory"} {
		if _, err := review.Approve(ctx, incident.ID(), actor); !domain.IsForbidden(err) {
			t.Errorf("%s: %v", name, err)
		}
		if _, err := review.SendBack(ctx, incident.ID(), actor, "x"); !domain.IsForbidden(err) {
			t.Errorf("%s send-back: %v", name, err)
		}
		if audit := auditNamed(t, h, "incident.postmortem.refused"); audit.Outcome != domain.AuditDenied || audit.Details["actor"] != actor {
			t.Errorf("%s: audit = %+v", name, audit)
		}
	}
	if _, err := review.Approve(ctx, incident.ID(), "two words"); err == nil {
		t.Error("an actor with a space")
	}
	unchanged, _ := h.incs.Get(ctx, incident.ID())
	if postmortemState(t, unchanged) != domain.PostmortemDraft {
		t.Fatalf("a refusal changed it: %+v", unchanged.Snapshot())
	}
	if final, err := review.Approve(ctx, incident.ID(), "cli:Tirso"); err != nil || postmortemState(t, final) != domain.PostmortemApproved {
		t.Fatalf("the approver: %v", err)
	}
	if audit := auditNamed(t, h, "incident.postmortem.approve"); audit.Actor != "cli:Tirso" {
		t.Errorf("audit = %+v", audit)
	}

	bare := newHarness()
	sev3 := resolvedIncidentOf(t, bare, postmortemTeam(t, bare, nil), "sev3")
	if _, err := NewReviewPostmortem(bare.deps).Approve(ctx, sev3.ID(), "tirso"); !isState(err) {
		t.Errorf("no postmortem: %v", err)
	}
	if _, err := review.Approve(ctx, "inc-none", "tirso"); !domain.IsNotFound(err) {
		t.Errorf("no incident: %v", err)
	}
}

func TestReviewAfterTheWriterIsGoneFailsClosed(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	triage := postmortemTeam(t, h, nil)
	incident := resolvedIncidentOf(t, h, triage, "sev1")
	draftOf(t, h, incident, "draft")
	if err := h.bots.Delete(ctx, "scribe"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewReviewPostmortem(h.deps).Approve(ctx, incident.ID(), "tirso"); !domain.IsForbidden(err) {
		t.Errorf("bot gone: %v", err)
	}
}

// A task that outlived its cycle moves nothing, and an alert within the
// window reopens as ever.
func TestAReopeningOutrunsARunningPostmortem(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	triage := postmortemTeam(t, h, nil)
	incident := resolvedIncidentOf(t, h, triage, "sev1")
	scribeTask := incident.CurrentTaskID()
	reopened, err := NewOpenIncident(h.deps).Execute(ctx, alert(triage, "fp-a"))
	if err != nil || !reopened.Reopened || reopened.Incident.Status() != domain.IncidentOpen {
		t.Fatalf("%v %+v", err, reopened)
	}
	h.runtime.report = completes("late", 0.03)
	if _, err := NewRunTask(h.deps).Execute(ctx, scribeTask); err != nil {
		t.Fatal(err)
	}
	moved, _ := h.incs.Get(ctx, incident.ID())
	if moved.Status() != domain.IncidentOpen || postmortemState(t, moved) != domain.PostmortemWriting {
		t.Fatalf("moved: %+v", moved.Snapshot())
	}
	if moved.CostUSD() < 0.03 {
		t.Errorf("cost = %v", moved.CostUSD())
	}
}

func TestAnAlertmanagerResolutionLaunchesItAndDeletingTheBotDoesNot(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	triage := teamBot(t, h, "triage", []string{"sre"}, func(d *BotDefinition) {
		d.Triggers = []domain.TriggerSnapshot{{Type: "manual"}, {Type: "incident"}, {Type: "alertmanager", Name: "am", Secret: "0123456789abcdef"}}
	})
	teamBot(t, h, "scribe", []string{"sre"}, func(d *BotDefinition) {
		d.Triggers = []domain.TriggerSnapshot{{Type: "incident", Phase: "postmortem", Severities: []string{"sev3"}}}
	})
	if _, err := NewOpenIncident(h.deps).Execute(ctx, OpenIncidentCommand{Tenant: "default", BotID: "triage", GroupKey: "g", Title: "t", Severity: "sev3"}); err != nil {
		t.Fatal(err)
	}
	incident, found, err := h.incs.FindNewestByGroupKey(ctx, "default", "g")
	if err != nil || !found {
		t.Fatal(err)
	}
	fired, err := NewFireTrigger(h.deps).resolveGroup(ctx, triage, mustTrigger(t, triage, "am"), "g", nil)
	if err != nil || fired == nil || postmortemState(t, *fired) != domain.PostmortemWriting {
		t.Fatalf("%v %+v", err, fired)
	}

	if _, err := NewOpenIncident(h.deps).Execute(ctx, OpenIncidentCommand{Tenant: "default", BotID: "triage", GroupKey: "h", Title: "t", Severity: "sev3"}); err != nil {
		t.Fatal(err)
	}
	other, _, _ := h.incs.FindNewestByGroupKey(ctx, "default", "h")
	if _, err := NewDeleteBot(h.deps).Execute(ctx, "triage"); err != nil {
		t.Fatal(err)
	}
	gone, _ := h.incs.Get(ctx, other.ID())
	if _, ok := gone.Postmortem(); ok || gone.Status() != domain.IncidentResolved {
		t.Errorf("deleted: %+v", gone.Snapshot())
	}
	_ = incident
}

func mustTrigger(t *testing.T, bot domain.Bot, name string) domain.Trigger {
	t.Helper()
	trigger, ok := bot.Triggers().Named(name)
	if !ok {
		t.Fatalf("no trigger %s", name)
	}
	return trigger
}

// Long texts are cut to fit an objective; a short one is never cut for
// them, and the input keeps them whole.
func TestAPostmortemObjectiveFits(t *testing.T) {
	h := newHarness()
	triage := postmortemTeam(t, h, nil)
	incident := resolvedIncidentOf(t, h, triage, "sev1")
	trigger := domain.Trigger{}
	brief := PostmortemInput{Diagnosis: strings.Repeat("d", 16000), Proposal: strings.Repeat("p", 16000), Review: &PostmortemReview{Attempt: 2, Note: "a short note", Draft: strings.Repeat("r", 32000)}}
	objective, err := postmortemObjective(trigger, incident, brief)
	if err != nil {
		t.Fatal(err)
	}
	if len(objective.String()) > domain.MaxObjectiveLength || !strings.Contains(objective.String(), "a short note") || !strings.Contains(objective.String(), "no verify task ran") || !strings.Contains(objective.String(), "No call waited") {
		t.Errorf("%d bytes: %.300s", len(objective.String()), objective)
	}
	custom, err := domain.NewTrigger(domain.TriggerSnapshot{Type: "incident", Phase: "postmortem", Objective: "Write {{.id}}: {{.nothing}}"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := postmortemObjective(custom, incident, brief); err == nil {
		t.Error("a template naming an unknown field rendered")
	}
	unknown := costLine(0.5, true)
	if !strings.Contains(unknown, "at least") {
		t.Errorf("cost = %q", unknown)
	}
}
