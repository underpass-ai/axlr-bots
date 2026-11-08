package application

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// brigade registers the sre team of the apply and verify phases: triage
// takes incidents; fix (act-with-approval, three tools) proposes at
// propose with one tool and applies at its own level with two; verify
// checks. approvals are fix's.
func brigade(t *testing.T, h *harness, approvals domain.ApprovalsSnapshot) domain.Bot {
	t.Helper()
	triage := teamBot(t, h, "triage", []string{"sre"}, func(d *BotDefinition) {
		d.Triggers = []domain.TriggerSnapshot{{Type: "manual"}, {Type: "incident"}}
	})
	teamBot(t, h, "fix", []string{"sre"}, func(d *BotDefinition) {
		d.Level = "act-with-approval"
		d.AllowedTools = []string{"local_read", "local_search", "local_write"}
		d.Approvals = approvals
		d.Triggers = []domain.TriggerSnapshot{
			{Type: "incident", Name: "fix", Phase: "fix", Level: "propose", Tools: []string{"local_read"}},
			{Type: "incident", Name: "apply", Phase: "apply", Tools: []string{"local_read", "local_write"}, Objective: "Apply for {{.id}}: {{.proposal}} (cause {{.diagnosis}})"},
		}
	})
	teamBot(t, h, "verify", []string{"sre"}, func(d *BotDefinition) {
		d.Triggers = []domain.TriggerSnapshot{{Type: "incident", Phase: "verify"}}
	})
	h.log.events, h.log.audits = nil, nil
	return triage
}

// proposed opens an incident and runs its triage and fix tasks: it is
// Proposed with its apply task attached and pending.
func proposed(t *testing.T, h *harness, triage domain.Bot) domain.Incident {
	t.Helper()
	ctx := context.Background()
	opened, err := NewOpenIncident(h.deps).Execute(ctx, alert(triage, "fp-a"))
	if err != nil {
		t.Fatal(err)
	}
	return throughFix(t, h, opened.Incident.ID())
}

// throughFix runs the incident's current triage task, then its fix task.
func throughFix(t *testing.T, h *harness, id domain.IncidentID) domain.Incident {
	t.Helper()
	ctx := context.Background()
	for _, answer := range []string{"CAUSE: deployment web drifted: replicas 1, desired 3", "PROPOSAL: kubectl apply -f desired/web.yaml\nROLLBACK: replicas 1"} {
		incident, _ := h.incs.Get(ctx, id)
		h.runtime.report = completes(answer, 0.01)
		if _, err := NewRunTask(h.deps).Execute(ctx, incident.CurrentTaskID()); err != nil {
			t.Fatal(err)
		}
	}
	incident, _ := h.incs.Get(ctx, id)
	if current, _ := incident.CurrentTask(); incident.Status() != domain.IncidentProposed || current.Phase() != domain.IncidentPhaseApply {
		t.Fatalf("not Proposed with an apply task: %+v", incident.Snapshot())
	}
	return incident
}

// waitingApply runs the incident's apply task until its write call waits.
func waitingApply(t *testing.T, h *harness, incident domain.Incident) domain.Task {
	t.Helper()
	h.runtime.report = ExecutionReport{Outcome: domain.ExecutionDecisionRequired, Result: domain.Result{SessionID: "sess-a", PendingDecision: &domain.PendingDecision{Tool: "local_write", CallID: "c-apply", SessionID: "sess-a"}}}
	res, err := NewRunTask(h.deps).Execute(context.Background(), incident.CurrentTaskID())
	if err != nil || res.Task.Status() != domain.TaskWaitingApproval {
		t.Fatalf("apply run: %v %v", err, res.Task.Status())
	}
	return res.Task
}

func lastOf(h *harness, kind domain.EventType) (domain.Event, bool) {
	for n := len(h.log.events) - 1; n >= 0; n-- {
		if h.log.events[n].Type == kind {
			return h.log.events[n], true
		}
	}
	return domain.Event{}, false
}

// Proposed launches the apply task once, under the phase's policy; the
// person's approval is Applying, the task's end Verifying and the verify
// task; a green verdict waits for the source, which resolves it.
func TestApplyAndVerifyPhases(t *testing.T) {
	h := newHarness()
	triage := brigade(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := proposed(t, h, triage)
	id := incident.ID().String()
	applyTask, err := h.tasks.Get(ctx, incident.CurrentTaskID())
	if err != nil {
		t.Fatal(err)
	}
	if applyTask.BotID() != "fix" || applyTask.IdempotencyKey() != domain.IdempotencyKey("incident:"+id+":1:apply") || applyTask.Trigger().Name != "apply" {
		t.Errorf("apply task = %+v", applyTask.Snapshot())
	}
	if objective := applyTask.Objective().String(); !strings.Contains(objective, "kubectl apply -f desired/web.yaml") || !strings.Contains(objective, "replicas 1, desired 3") {
		t.Errorf("apply objective = %q", objective)
	}
	// The fix task ran narrowed to propose and one tool.
	fix := h.runtime.requests[len(h.runtime.requests)-1]
	if fix.Level != domain.AutonomyPropose || !slices.Equal(fix.AllowedTools, []string{"local_read"}) {
		t.Errorf("fix ran at %s with %v", fix.Level, fix.AllowedTools)
	}
	// A repeat while Proposed, and another launch, start nothing more.
	tasks := len(h.tasks.tasks)
	if again, err := NewOpenIncident(h.deps).Execute(ctx, alert(triage, "fp-a")); err != nil || again.Action != IncidentRepeated || len(h.tasks.tasks) != tasks {
		t.Fatalf("repeat while Proposed: %+v %v", again, err)
	}
	if _, err := newPhaseLauncher(h.deps).launch(ctx, incident, ""); err != nil || len(h.tasks.tasks) != tasks {
		t.Fatalf("a second launch: %v", err)
	}

	waiting := waitingApply(t, h, incident)
	run := h.runtime.requests[len(h.runtime.requests)-1]
	if run.Level != domain.AutonomyActWithApproval || slices.Contains(run.AllowedTools, "local_search") || !slices.Contains(run.AllowedTools, "local_write") {
		t.Errorf("apply ran at %s with %v", run.Level, run.AllowedTools)
	}
	if still, _ := h.incs.Get(ctx, incident.ID()); still.Status() != domain.IncidentProposed {
		t.Fatalf("a waiting apply moved the incident: %s", still.Status())
	}

	h.decider.report = completes("APPLIED: kubectl apply -f desired/web.yaml", 0.02)
	h.clock.now = h.clock.now.Add(time.Minute)
	if _, err := NewApproveTask(h.deps).Execute(ctx, waiting.ID(), DecisionApprove, "", ""); err != nil {
		t.Fatal(err)
	}
	decide := h.decider.requests[len(h.decider.requests)-1]
	if decide.Level != domain.AutonomyActWithApproval || slices.Contains(decide.AllowedTools, "local_search") {
		t.Errorf("the decision ran at %s with %v", decide.Level, decide.AllowedTools)
	}
	applying, ok := lastOf(h, domain.EventIncidentApplying)
	if !ok || applying.Payload["callId"] != "c-apply" || applying.Payload["decidedBy"] != h.deps.Actor || applying.Payload["tool"] != "local_write" {
		t.Errorf("incident.applying = %+v", applying)
	}
	verifying, ok := lastOf(h, domain.EventIncidentVerifying)
	if !ok || verifying.Payload["status"] != "Verifying" {
		t.Errorf("incident.verifying = %+v (events %v)", verifying, h.log.eventTypes())
	}
	types := strings.Join(h.log.eventTypes(), ",")
	if strings.Index(types, "incident.applying") > strings.Index(types, "incident.verifying") {
		t.Errorf("events out of order: %s", types)
	}
	incident, _ = h.incs.Get(ctx, incident.ID())
	current, _ := incident.CurrentTask()
	if incident.Status() != domain.IncidentVerifying || current.Phase() != domain.IncidentPhaseVerify || current.BotID() != "verify" {
		t.Fatalf("after apply: %+v", incident.Snapshot())
	}
	verifyTask, _ := h.tasks.Get(ctx, current.TaskID())
	if objective := verifyTask.Objective().String(); verifyTask.IdempotencyKey() != domain.IdempotencyKey("incident:"+id+":1:verify") || !strings.Contains(objective, "VERDICT: red") || !strings.Contains(objective, "desired/web.yaml") {
		t.Errorf("verify task %s: %q", verifyTask.IdempotencyKey(), objective)
	}

	h.runtime.report = completes("The rollout is done.\nVERDICT: green", 0.01)
	if _, err := NewRunTask(h.deps).Execute(ctx, verifyTask.ID()); err != nil {
		t.Fatal(err)
	}
	incident, _ = h.incs.Get(ctx, incident.ID())
	if incident.Status() != domain.IncidentVerifying || incident.CostUSD() < 0.049 {
		t.Fatalf("after a green verify: %s %v", incident.Status(), incident.CostUSD())
	}
	if audit := h.log.audits[len(h.log.audits)-1]; audit.Action != "incident.advance" || audit.Details["verdict"] != "green" {
		t.Errorf("audit = %+v", audit)
	}
	resolved, err := NewResolveIncident(h.deps).Execute(ctx, incident.ID(), "alerts green")
	if err != nil || resolved.Status() != domain.IncidentResolved {
		t.Fatalf("resolve: %v", err)
	}
}

// A red verdict sends the incident back to Open in a new cycle, and the
// next alert starts its triage.
func TestARedVerifySendsTheIncidentBack(t *testing.T) {
	h := newHarness()
	triage := brigade(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := proposed(t, h, triage)
	// Applied without a wait (the apply asked nobody): Applying, then Verifying.
	h.runtime.report = completes("APPLIED", 0)
	if _, err := NewRunTask(h.deps).Execute(ctx, incident.CurrentTaskID()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h.log.eventTypes(), ","); !strings.Contains(got, "incident.applying,incident.verifying") {
		t.Errorf("events = %s", got)
	}
	incident, _ = h.incs.Get(ctx, incident.ID())
	h.runtime.report = completes("VERDICT: red\nreplicas still 1", 0)
	if _, err := NewRunTask(h.deps).Execute(ctx, incident.CurrentTaskID()); err != nil {
		t.Fatal(err)
	}
	back, _ := h.incs.Get(ctx, incident.ID())
	if back.Status() != domain.IncidentOpen || back.Cycle() != 2 || !strings.Contains(back.Reason(), "still red") || back.SendBacks() != 1 {
		t.Fatalf("after a red verify: %+v", back.Snapshot())
	}
	// The new cycle's triage starts at once, without waiting for an alert.
	triageTask, err := h.tasks.Get(ctx, back.CurrentTaskID())
	if err != nil || triageTask.IdempotencyKey() != domain.IdempotencyKey("incident:"+back.ID().String()+":2") || triageTask.BotID() != "triage" {
		t.Fatalf("the new cycle's triage: %+v %v", triageTask, err)
	}
	reopened, ok := lastOf(h, domain.EventIncidentReopened)
	if !ok || reopened.Payload["from"] != "Verifying" || reopened.Payload["cycle"] != 2 || reopened.Payload["verdict"] != "red" ||
		reopened.Payload["taskId"] != triageTask.ID().String() || reopened.Payload["endedTaskId"] == nil {
		t.Errorf("incident.reopened = %+v", reopened)
	}
	if count := strings.Count(strings.Join(h.log.eventTypes(), ","), "incident.reopened"); count != 1 {
		t.Errorf("incident.reopened published %d times", count)
	}
	tasks := len(h.tasks.tasks)
	if again, err := NewOpenIncident(h.deps).Execute(ctx, alert(triage, "fp-a")); err != nil || again.Action != IncidentRepeated || len(h.tasks.tasks) != tasks {
		t.Fatalf("the next alert: %+v %v", again, err)
	}
}

// A denial sends the incident back to Open; the session goes on and its
// task's end moves nothing. The second denial, here an expiry, escalates
// it, and an alert then is only a repeat.
func TestDeniedTwiceTheIncidentIsLeftToAPerson(t *testing.T) {
	h := newHarness()
	triage := brigade(t, h, domain.ApprovalsSnapshot{TTL: "2m"})
	ctx := context.Background()
	incident := proposed(t, h, triage)
	waiting := waitingApply(t, h, incident)
	h.decider.report = completes("Not applied: the call was denied.", 0.01)
	if _, err := NewApproveTask(h.deps).Execute(ctx, waiting.ID(), DecisionDeny, "", ""); err != nil {
		t.Fatal(err)
	}
	back, _ := h.incs.Get(ctx, incident.ID())
	if back.Status() != domain.IncidentOpen || back.Cycle() != 2 || back.SendBacks() != 1 || !strings.Contains(back.Reason(), "local_write") || !strings.Contains(back.Reason(), "denied by "+h.deps.Actor) {
		t.Fatalf("after a denial: %+v", back.Snapshot())
	}
	if reopened, ok := lastOf(h, domain.EventIncidentReopened); !ok || reopened.Payload["from"] != "Proposed" || reopened.Payload["sendBacks"] != 1 {
		t.Errorf("incident.reopened = %+v", reopened)
	}
	if _, ok := lastOf(h, domain.EventIncidentVerifying); ok {
		t.Error("the denied apply's end moved the incident to Verifying")
	}
	if audit := h.log.audits[len(h.log.audits)-1]; audit.Action != "incident.advance" || audit.Outcome != domain.AuditDenied {
		t.Errorf("the stale end was not audited: %+v", audit)
	}
	if back.CostUSD() < 0.029 {
		t.Errorf("the denied apply's cost was lost: %v", back.CostUSD())
	}

	if current, _ := back.CurrentTask(); current.Phase() != domain.IncidentPhaseTriage || current.Cycle() != 2 {
		t.Fatalf("the new cycle's triage did not start: %+v", back.Snapshot())
	}
	incident = throughFix(t, h, incident.ID())
	if incident.Cycle() != 2 {
		t.Fatalf("cycle %d", incident.Cycle())
	}
	waitingApply(t, h, incident)
	h.decider.report = completes("Not applied.", 0)
	h.clock.now = h.clock.now.Add(time.Hour)
	if done, err := NewSerialRunner(h.deps).ExpireApprovals(ctx); err != nil || len(done) != 1 {
		t.Fatalf("expire: %v %+v", err, done)
	}
	escalated, _ := h.incs.Get(ctx, incident.ID())
	if escalated.Status() != domain.IncidentEscalated || escalated.SendBacks() != domain.IncidentMaxSendBacks || !strings.Contains(escalated.Reason(), "expired") || !strings.Contains(escalated.Reason(), "left to a person") {
		t.Fatalf("after the second denial: %+v", escalated.Snapshot())
	}
	if _, ok := lastOf(h, domain.EventIncidentEscalated); !ok {
		t.Errorf("events = %v", h.log.eventTypes())
	}
	tasks := len(h.tasks.tasks)
	if last, err := NewOpenIncident(h.deps).Execute(ctx, alert(triage, "fp-a")); err != nil || last.Action != IncidentRepeated || len(h.tasks.tasks) != tasks {
		t.Errorf("an alert after two sendBacks: %+v %v", last, err)
	}
}

// An expiry that cancels the task is a denial too; a failed apply sends
// the incident back without counting one.
func TestAnExpiredOrFailedApply(t *testing.T) {
	h := newHarness()
	triage := brigade(t, h, domain.ApprovalsSnapshot{TTL: "1m", OnExpire: "cancel"})
	ctx := context.Background()
	incident := proposed(t, h, triage)
	waitingApply(t, h, incident)
	h.clock.now = h.clock.now.Add(time.Hour)
	if done, err := NewSerialRunner(h.deps).ExpireApprovals(ctx); err != nil || len(done) != 1 || done[0].Task.Status() != domain.TaskExpired {
		t.Fatalf("expire: %v %+v", err, done)
	}
	back, _ := h.incs.Get(ctx, incident.ID())
	if back.Status() != domain.IncidentOpen || back.SendBacks() != 1 || !strings.Contains(back.Reason(), "expired") {
		t.Fatalf("after a cancelling expiry: %+v", back.Snapshot())
	}

	h2 := newHarness()
	triage = brigade(t, h2, domain.ApprovalsSnapshot{})
	incident = proposed(t, h2, triage)
	h2.runtime.report = ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "kube-mcp: connection refused"}
	if _, err := NewRunTask(h2.deps).Execute(ctx, incident.CurrentTaskID()); err != nil {
		t.Fatal(err)
	}
	failed, _ := h2.incs.Get(ctx, incident.ID())
	if failed.Status() != domain.IncidentOpen || failed.SendBacks() != 1 || failed.Cycle() != 2 || !strings.HasPrefix(failed.Reason(), "apply task ") || !strings.Contains(failed.Reason(), "connection refused") {
		t.Fatalf("after a failed apply: %+v", failed.Snapshot())
	}
}

// A verify task that fails escalates the incident; a decision on a
// task that is not the incident's apply task moves nothing.
func TestAFailedVerifyAndForeignDecisions(t *testing.T) {
	h := newHarness()
	triage := brigade(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := proposed(t, h, triage)
	fixTask := incident.Tasks()[1]
	task, _ := h.tasks.Get(ctx, fixTask.TaskID())
	before := len(h.log.events)
	newIncidentAdvancer(h.deps).taskDecided(ctx, task, domain.ApprovalApproved, "c", "local_write", "ops", true)
	if len(h.log.events) != before {
		t.Error("a decision on the fix task moved the incident")
	}
	newIncidentAdvancer(h.deps).taskDecided(ctx, domain.Task{}, domain.ApprovalApproved, "c", "", "ops", true)
	applyTask, _ := h.tasks.Get(ctx, incident.CurrentTaskID())
	h.incs.fail = errBoom
	newIncidentAdvancer(h.deps).taskDecided(ctx, applyTask, domain.ApprovalApproved, "c", "", "ops", true)
	h.incs.fail = nil
	if audit := h.log.audits[len(h.log.audits)-1]; audit.Outcome != domain.AuditDenied || audit.Details["decision"] != "approved" {
		t.Errorf("a store that failed was not audited: %+v", audit)
	}
	h.runtime.report = completes("APPLIED", 0)
	if _, err := NewRunTask(h.deps).Execute(ctx, incident.CurrentTaskID()); err != nil {
		t.Fatal(err)
	}
	incident, _ = h.incs.Get(ctx, incident.ID())
	h.runtime.report = ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "no model"}
	if _, err := NewRunTask(h.deps).Execute(ctx, incident.CurrentTaskID()); err != nil {
		t.Fatal(err)
	}
	escalated, _ := h.incs.Get(ctx, incident.ID())
	if escalated.Status() != domain.IncidentEscalated || !strings.HasPrefix(escalated.Reason(), "verify task ") {
		t.Fatalf("after a failed verify: %+v", escalated.Snapshot())
	}
}

// An incident trigger may narrow its phase's policy, never widen it.
func TestAPhasePolicyThatWidensIsRefused(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	for name, trigger := range map[string]domain.TriggerSnapshot{
		"a higher level":   {Type: "incident", Phase: "apply", Level: "act-within-policy"},
		"a tool not given": {Type: "incident", Phase: "apply", Tools: []string{"local_exec"}},
	} {
		d := sampleDefinition()
		d.ID, d.Name, d.Level = "wide", "wide", "act-with-approval"
		d.Triggers = []domain.TriggerSnapshot{trigger}
		if _, err := NewRegisterBot(h.deps).Execute(ctx, d); !isValidation(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	d := sampleDefinition()
	d.ID, d.Name, d.Level = "narrow", "narrow", "act-with-approval"
	d.Triggers = []domain.TriggerSnapshot{{Type: "incident", Phase: "apply", Level: "act-with-approval", Tools: []string{"kubernetes.get_pods"}}}
	bot, err := NewRegisterBot(h.deps).Execute(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	lower := "propose"
	if _, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Level: &lower}); !isValidation(err) {
		t.Errorf("a level below its trigger's: %v", err)
	}
	fewer := []string{"observability.query_logs"}
	if _, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{AllowedTools: &fewer}); !isValidation(err) {
		t.Errorf("tools that strand its trigger's: %v", err)
	}
}

func isValidation(err error) bool {
	var invalid domain.ValidationError
	return errors.As(err, &invalid)
}

// On a node, the approval moves the incident when it is queued, before
// the node runs it, and both the run and the decision carry the phase's
// policy.
func TestApplyOnANode(t *testing.T) {
	h := newHarness()
	triage := brigade(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := proposed(t, h, triage)
	node := onlineNode(t, h, "web-1")
	first, found, err := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	if err != nil || !found || first.Run == nil || first.Run.TaskID != incident.CurrentTaskID() {
		t.Fatalf("assignment: %v %+v", err, first)
	}
	if first.Run.Level != domain.AutonomyActWithApproval || slices.Contains(first.Run.AllowedTools, "local_search") || !slices.Contains(first.Run.AllowedTools, "local_write") {
		t.Errorf("the node runs the apply at %s with %v", first.Run.Level, first.Run.AllowedTools)
	}
	pending := ExecutionReport{Outcome: domain.ExecutionDecisionRequired, Result: domain.Result{SessionID: "sess-n", PendingDecision: &domain.PendingDecision{Tool: "local_write", CallID: "c-n", SessionID: "sess-n"}}}
	if _, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), first.ExecutionID, pending); err != nil {
		t.Fatal(err)
	}
	if _, err := NewApproveTask(h.deps).Execute(ctx, incident.CurrentTaskID(), DecisionApprove, "c-n", ""); err != nil {
		t.Fatal(err)
	}
	if queued, _ := h.incs.Get(ctx, incident.ID()); queued.Status() != domain.IncidentApplying {
		t.Fatalf("a queued approval left the incident %s", queued.Status())
	}
	decide, found, err := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	if err != nil || !found || decide.Decide == nil || decide.Decide.Level != domain.AutonomyActWithApproval || slices.Contains(decide.Decide.AllowedTools, "local_search") {
		t.Fatalf("decision assignment: %v %+v", err, decide)
	}
	if _, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), decide.ExecutionID, completes("APPLIED", 0.01)); err != nil {
		t.Fatal(err)
	}
	if verifying, _ := h.incs.Get(ctx, incident.ID()); verifying.Status() != domain.IncidentVerifying {
		t.Fatalf("after the node's apply: %s", verifying.Status())
	}
}

// With no teammate taking the verify phase, the incident waits in
// Verifying for the source to resolve it.
func TestWithoutAVerifyBotTheIncidentWaitsInVerifying(t *testing.T) {
	h := newHarness()
	triage := brigade(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := proposed(t, h, triage)
	if _, err := NewDeleteBot(h.deps).Execute(ctx, "verify"); err != nil {
		t.Fatal(err)
	}
	h.runtime.report = completes("APPLIED", 0)
	if _, err := NewRunTask(h.deps).Execute(ctx, incident.CurrentTaskID()); err != nil {
		t.Fatal(err)
	}
	waiting, _ := h.incs.Get(ctx, incident.ID())
	if current, _ := waiting.CurrentTask(); waiting.Status() != domain.IncidentVerifying || current.Phase() != domain.IncidentPhaseApply {
		t.Fatalf("without a verify bot: %+v", waiting.Snapshot())
	}
}

// An incident read on its own says whether it needs a person, its
// current task and the approvals its tasks waited for; a list says only
// whether it needs a person, from one read of the waiting tasks.
func TestAnIncidentSaysWhenItNeedsAPerson(t *testing.T) {
	h := newHarness()
	triage := brigade(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := proposed(t, h, triage)
	report, err := NewGetIncident(h.deps).Execute(ctx, incident.ID())
	if err != nil || report.NeedsPerson || report.Current == nil || report.Current.Status() != domain.TaskPending || report.Approvals != (IncidentApprovals{}) {
		t.Fatalf("pending apply: %+v %v", report, err)
	}
	waiting := waitingApply(t, h, incident)
	report, _ = NewGetIncident(h.deps).Execute(ctx, incident.ID())
	if !report.NeedsPerson || report.Current.ID() != waiting.ID() || report.Approvals.Waiting != 1 {
		t.Fatalf("waiting apply: %+v", report)
	}
	list, err := NewListIncidents(h.deps).Execute(ctx, IncidentQuery{})
	if err != nil || len(list) != 1 || !list[0].NeedsPerson || list[0].Current != nil {
		t.Fatalf("list: %+v %v", list, err)
	}
	h.decider.report = completes("Not applied.", 0)
	if _, err := NewApproveTask(h.deps).Execute(ctx, waiting.ID(), DecisionDeny, "", ""); err != nil {
		t.Fatal(err)
	}
	report, _ = NewGetIncident(h.deps).Execute(ctx, incident.ID())
	if report.NeedsPerson || report.Approvals.Denied != 1 || report.Approvals.Waiting != 0 {
		t.Fatalf("after a denial: %+v", report)
	}
	h.tasks.listFail = errBoom
	if _, err := NewListIncidents(h.deps).Execute(ctx, IncidentQuery{}); !errors.Is(err, errBoom) {
		t.Errorf("a task store that fails: %v", err)
	}
	h.tasks.listFail = nil
	if _, err := NewGetIncident(h.deps).Execute(ctx, "inc-none"); !domain.IsNotFound(err) {
		t.Errorf("a missing incident: %v", err)
	}
}

// A send-back whose triage cannot start (its bot paused) is published
// without a task, and the next alert of the group starts it.
func TestASentBackIncidentWhoseTriageCannotStartWaitsForTheNextAlert(t *testing.T) {
	h := newHarness()
	triage := brigade(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := proposed(t, h, triage)
	if _, err := NewPauseBot(h.deps).Execute(ctx, triage.ID()); err != nil {
		t.Fatal(err)
	}
	h.runtime.report = ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "rollout failed"}
	if _, err := NewRunTask(h.deps).Execute(ctx, incident.CurrentTaskID()); err != nil {
		t.Fatal(err)
	}
	back, _ := h.incs.Get(ctx, incident.ID())
	if back.Status() != domain.IncidentOpen || back.HasTask(domain.IncidentPhaseTriage) {
		t.Fatalf("after a failed apply with triage paused: %+v", back.Snapshot())
	}
	if reopened, ok := lastOf(h, domain.EventIncidentReopened); !ok || reopened.Payload["from"] != "Proposed" {
		t.Errorf("incident.reopened = %+v", reopened)
	}
	if _, err := NewEnableBot(h.deps).Execute(ctx, triage.ID()); err != nil {
		t.Fatal(err)
	}
	again, err := NewOpenIncident(h.deps).Execute(ctx, alert(triage, "fp-a"))
	if err != nil || again.Task == nil || again.Task.IdempotencyKey() != domain.IdempotencyKey("incident:"+back.ID().String()+":2") {
		t.Fatalf("the next alert: %+v %v", again, err)
	}
}

// Every send-back counts: an apply that keeps failing at once is
// escalated at the second, never looping triage, fix and apply (review
// of #133, R1).
func TestAnApplyThatKeepsFailingIsEscalated(t *testing.T) {
	h := newHarness()
	triage := brigade(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := proposed(t, h, triage)
	for n := 1; n <= domain.IncidentMaxSendBacks; n++ {
		h.runtime.report = ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "kube-mcp down"}
		if _, err := NewRunTask(h.deps).Execute(ctx, incident.CurrentTaskID()); err != nil {
			t.Fatal(err)
		}
		back, _ := h.incs.Get(ctx, incident.ID())
		if n < domain.IncidentMaxSendBacks {
			if back.Status() != domain.IncidentOpen || back.SendBacks() != n {
				t.Fatalf("send-back %d: %+v", n, back.Snapshot())
			}
			incident = throughFix(t, h, incident.ID())
			continue
		}
		if back.Status() != domain.IncidentEscalated || !strings.HasSuffix(back.Reason(), "left to a person)") || back.CanRetry() {
			t.Fatalf("send-back %d: %+v", n, back.Snapshot())
		}
		tasks := len(h.tasks.tasks)
		if again, err := NewOpenIncident(h.deps).Execute(ctx, alert(triage, "fp-a")); err != nil || again.Action != IncidentRepeated || len(h.tasks.tasks) != tasks {
			t.Errorf("an alert after the cap: %+v %v", again, err)
		}
	}
}

// A denied apply whose session asks again is cancelled, and one that
// still waits can be denied but never approved once the incident went on
// (review of #133, R6).
func TestAStaleApplyIsNeverApproved(t *testing.T) {
	h := newHarness()
	triage := brigade(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := proposed(t, h, triage)
	waiting := waitingApply(t, h, incident)
	h.decider.report = ExecutionReport{Outcome: domain.ExecutionDecisionRequired, Result: domain.Result{SessionID: "sess-a", PendingDecision: &domain.PendingDecision{Tool: "local_write", CallID: "c-again", SessionID: "sess-a"}}}
	if _, err := NewApproveTask(h.deps).Execute(ctx, waiting.ID(), DecisionDeny, "", ""); err != nil {
		t.Fatal(err)
	}
	stale, _ := h.tasks.Get(ctx, waiting.ID())
	if stale.Status() != domain.TaskCancelled {
		t.Fatalf("the denied apply asking again is %s", stale.Status())
	}
	decisions := len(h.decider.requests)
	if _, err := NewApproveTask(h.deps).Execute(ctx, waiting.ID(), DecisionApprove, "c-again", ""); err == nil || len(h.decider.requests) != decisions {
		t.Fatalf("a stale apply was approved: %v", err)
	}
	// An apply still waiting when its incident is resolved by hand.
	h2 := newHarness()
	triage = brigade(t, h2, domain.ApprovalsSnapshot{})
	incident = proposed(t, h2, triage)
	waiting = waitingApply(t, h2, incident)
	if _, err := NewResolveIncident(h2.deps).Execute(ctx, incident.ID(), "fixed by hand"); err != nil {
		t.Fatal(err)
	}
	var state domain.StateError
	if _, err := NewApproveTask(h2.deps).Execute(ctx, waiting.ID(), DecisionApprove, "", ""); !errors.As(err, &state) || !strings.Contains(err.Error(), "no longer stands on") {
		t.Errorf("approve after the resolution: %v", err)
	}
	if audit := h2.log.audits[len(h2.log.audits)-1]; audit.Action != "task.decide" || audit.Outcome != domain.AuditDenied {
		t.Errorf("the refusal was not audited: %+v", audit)
	}
	h2.decider.report = completes("not applied", 0)
	if _, err := NewApproveTask(h2.deps).Execute(ctx, waiting.ID(), DecisionDeny, "", ""); err != nil {
		t.Errorf("deny after the resolution: %v", err)
	}
}

// An apply that starts waiting after its incident was resolved asks
// nobody (re-check of #133, N3).
func TestAWaitOfAnIncidentThatMovedOnNotifiesNobody(t *testing.T) {
	h := newHarness()
	triage := brigade(t, h, domain.ApprovalsSnapshot{Notify: []domain.NotifyTargetSnapshot{{Type: "webhook", URL: "https://example.test/hook", Secret: "0123456789abcdef-hook"}}})
	ctx := context.Background()
	incident := proposed(t, h, triage)
	if _, err := NewResolveIncident(h.deps).Execute(ctx, incident.ID(), "green"); err != nil {
		t.Fatal(err)
	}
	waitingApply(t, h, incident)
	if len(h.outbox.deliveries) != 0 {
		t.Errorf("notified %d", len(h.outbox.deliveries))
	}
	if audit := h.log.audits[len(h.log.audits)-1]; audit.Action != "approval.notify" || audit.Outcome != domain.AuditDenied {
		t.Errorf("audit = %+v", audit)
	}
}
