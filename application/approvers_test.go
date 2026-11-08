package application

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// auditsOf lists the audits of an action.
func auditsOf(h *harness, action string) []domain.AuditEntry {
	var out []domain.AuditEntry
	for _, audit := range h.log.audits {
		if audit.Action == action {
			out = append(out, audit)
		}
	}
	return out
}

// approvers lists the actors of a wait's approvals, in order.
func approvers(wait domain.ApprovalWait) []string {
	var out []string
	for _, mark := range wait.ApprovedBy {
		out = append(out, mark.Actor)
	}
	return out
}

// incidentIn opens an incident of the severity in the namespace and runs
// it to its apply call's wait.
func incidentIn(t *testing.T, h *harness, approvals domain.ApprovalsSnapshot, namespace, severity string) (domain.Incident, domain.Task) {
	t.Helper()
	triage := brigade(t, h, approvals)
	command := alert(triage, "fp-a")
	command.Namespace, command.Severity = namespace, severity
	opened, err := NewOpenIncident(h.deps).Execute(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Incident.Namespace() != namespace {
		t.Fatalf("namespace = %q", opened.Incident.Namespace())
	}
	incident := throughFix(t, h, opened.Incident.ID())
	task := waitingApply(t, h, incident)
	h.log.events, h.log.audits = nil, nil
	return incident, task
}

// A bot that lists approvers takes decisions from them only, approve and
// deny alike, and from a named actor only; each refusal is audited with
// the actor and changes nothing.
func TestApproversRefuseAnActorTheyDoNotList(t *testing.T) {
	h := newHarness()
	_, task := waitingWith(t, h, domain.ApprovalsSnapshot{Approvers: []string{"tirso", "oncall"}})
	ctx := context.Background()
	uc := NewApproveTask(h.deps)
	for _, c := range []struct{ decision, actor string }{
		{DecisionApprove, "mallory"}, {DecisionDeny, "mallory"}, {DecisionApprove, ""}, {DecisionDeny, ""}, {DecisionApprove, "cli:tirso"},
	} {
		_, err := uc.Execute(ctx, task.ID(), c.decision, "", c.actor)
		if !domain.IsForbidden(err) {
			t.Errorf("%s by %q: %v", c.decision, c.actor, err)
		}
	}
	refused := auditsOf(h, "approval.refused")
	if len(refused) != 5 || refused[0].Actor != "mallory" || refused[0].Details["actor"] != "mallory" || refused[1].Details["decision"] != DecisionDeny || refused[2].Details["actor"] != "" || refused[0].Outcome != domain.AuditDenied {
		t.Fatalf("refusals = %+v", refused)
	}
	if len(h.decider.requests) != 0 || len(h.log.events) != 0 {
		t.Fatal("a refused decision ran or published")
	}
	if _, err := uc.Execute(ctx, task.ID(), DecisionApprove, "", "bad actor"); err == nil {
		t.Error("an actor with a space was taken")
	}
	h.decider.report = ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done", SessionID: "sess-1"}}
	res, err := uc.Execute(ctx, task.ID(), DecisionApprove, "", "oncall")
	if err != nil || res.Task.Status() != domain.TaskCompleted || len(h.decider.requests) != 1 {
		t.Fatalf("approve by an approver: %v %v", err, res.Task.Status())
	}
	decided, _ := lastOf(h, domain.EventTaskDecided)
	if decided.Payload["decidedBy"] != "oncall" {
		t.Errorf("decided = %+v", decided.Payload)
	}
	if audits := auditsOf(h, "task.decide"); len(audits) == 0 || audits[len(audits)-1].Actor != "oncall" {
		t.Errorf("the decision was audited as %+v", audits)
	}
	if wait, _ := res.Task.Approval(); wait.ApprovalsNeeded != 0 || wait.ApprovedBy != nil {
		t.Errorf("a one-person wait grew two-person fields: %+v", wait)
	}
}

// A bot without approvers decides as before: any actor, and none is the
// control plane's own.
func TestWithoutApproversAnyoneDecides(t *testing.T) {
	h := newHarness()
	_, task := waitingWith(t, h, domain.ApprovalsSnapshot{})
	h.decider.report = ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done", SessionID: "sess-1"}}
	if _, err := NewApproveTask(h.deps).Execute(context.Background(), task.ID(), DecisionDeny, "", ""); err != nil {
		t.Fatal(err)
	}
	decided, _ := lastOf(h, domain.EventTaskDecided)
	if decided.Payload["decidedBy"] != "test:operator" {
		t.Errorf("decidedBy = %v", decided.Payload["decidedBy"])
	}
}

// An incident in a namespace twoPersons names waits for two different
// people: the first approval is recorded and runs nothing, the same
// person again and a nameless approval are refused, a second person runs
// the call and moves the incident.
func TestTwoPersonsInANamespace(t *testing.T) {
	h := newHarness()
	incident, task := incidentIn(t, h, domain.ApprovalsSnapshot{TwoPersons: &domain.TwoPersonsSnapshot{Namespaces: []string{"prod", "shop-prod"}}}, "shop-prod", "sev3")
	ctx := context.Background()
	if wait, _ := task.Approval(); wait.Needed() != 2 || wait.ApprovalsNeeded != 2 {
		t.Fatalf("wait = %+v", wait)
	}
	uc := NewApproveTask(h.deps)
	h.clock.now = h.clock.now.Add(time.Minute)
	first, err := uc.Execute(ctx, task.ID(), DecisionApprove, "c-apply", "tirso")
	if err != nil {
		t.Fatal(err)
	}
	wait, _ := first.Task.Approval()
	if first.Task.Status() != domain.TaskWaitingApproval || !wait.Open() || !slices.Equal(approvers(wait), []string{"tirso"}) || !wait.ApprovedBy[0].At.Equal(h.clock.now) || first.Execution.ID() != task.CurrentExecutionID() {
		t.Fatalf("first approval: %s %+v", first.Task.Status(), wait)
	}
	if len(h.decider.requests) != 0 {
		t.Fatal("the first approval ran the call")
	}
	if got, _ := h.incs.Get(ctx, incident.ID()); got.Status() != domain.IncidentProposed {
		t.Fatalf("the first approval moved the incident to %s", got.Status())
	}
	event, ok := lastOf(h, domain.EventApprovalFirst)
	if !ok || event.Payload["approvedBy"] != "tirso" || event.Payload["approvalsNeeded"] != 2 || event.ExecutionID != task.CurrentExecutionID() {
		t.Errorf("first event = %+v", event)
	}
	if audits := auditsOf(h, "approvals.first"); len(audits) != 1 || audits[0].Actor != "tirso" || audits[0].Details["actor"] != "tirso" || audits[0].Details["at"] == "" || audits[0].Details["approvals"] != "1 of 2" {
		t.Errorf("first audit = %+v", audits)
	}
	stored, _ := h.tasks.Get(ctx, task.ID())
	if wait, _ := stored.Approval(); len(wait.ApprovedBy) != 1 || !wait.Deadline.Equal(task.Snapshot().Approval.Deadline) {
		t.Errorf("stored wait = %+v", wait)
	}
	// The same person twice, or nobody named, is not a second person.
	var state domain.StateError
	if _, err := uc.Execute(ctx, task.ID(), DecisionApprove, "", "tirso"); !errors.As(err, &state) {
		t.Errorf("same actor again: %v", err)
	}
	if _, err := uc.Execute(ctx, task.ID(), DecisionApprove, "", ""); !domain.IsForbidden(err) {
		t.Errorf("nameless second approval: %v", err)
	}
	if refused := auditsOf(h, "approval.refused"); len(refused) != 2 || refused[0].Details["actor"] != "tirso" {
		t.Errorf("refusals = %+v", refused)
	}
	if len(h.decider.requests) != 0 {
		t.Fatal("a refused approval ran the call")
	}
	h.decider.report = ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "applied", SessionID: "sess-a"}}
	second, err := uc.Execute(ctx, task.ID(), DecisionApprove, "", "oncall")
	if err != nil || len(h.decider.requests) != 1 || h.decider.requests[0].Decision != DecisionApprove {
		t.Fatalf("second approval: %v %d", err, len(h.decider.requests))
	}
	closed, _ := second.Task.Approval()
	if closed.Outcome != domain.ApprovalApproved || !slices.Equal(approvers(closed), []string{"tirso", "oncall"}) {
		t.Errorf("closed wait = %+v", closed)
	}
	decided, _ := lastOf(h, domain.EventTaskDecided)
	if decided.Payload["decidedBy"] != "oncall" || !slices.Equal(decided.Payload["approvedBy"].([]string), []string{"tirso", "oncall"}) {
		t.Errorf("decided = %+v", decided.Payload)
	}
	if got, _ := h.incs.Get(ctx, incident.ID()); got.Status() == domain.IncidentProposed || got.Status() == domain.IncidentOpen {
		t.Errorf("the second approval left the incident %s", got.Status())
	}
	if _, err := domain.RestoreTask(second.Task.Snapshot()); err != nil {
		t.Errorf("restore: %v", err)
	}
}

// A denial after the first approval ends the wait as a denial, by anyone
// who may decide.
func TestTwoPersonsADenialEndsTheWait(t *testing.T) {
	h := newHarness()
	incident, task := incidentIn(t, h, domain.ApprovalsSnapshot{Approvers: []string{"tirso", "oncall"}, TwoPersons: &domain.TwoPersonsSnapshot{Severities: []string{"sev1", "sev2"}}}, "", "sev1")
	ctx := context.Background()
	uc := NewApproveTask(h.deps)
	if _, err := uc.Execute(ctx, task.ID(), DecisionApprove, "", "tirso"); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Execute(ctx, task.ID(), DecisionDeny, "", "mallory"); !domain.IsForbidden(err) {
		t.Errorf("deny by a stranger: %v", err)
	}
	h.decider.report = ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "not applied", SessionID: "sess-a"}}
	denied, err := uc.Execute(ctx, task.ID(), DecisionDeny, "", "tirso")
	if err != nil || len(h.decider.requests) != 1 || h.decider.requests[0].Decision != DecisionDeny {
		t.Fatalf("deny: %v %+v", err, h.decider.requests)
	}
	wait, _ := denied.Task.Approval()
	if wait.Outcome != domain.ApprovalDenied || !slices.Equal(approvers(wait), []string{"tirso"}) {
		t.Errorf("wait = %+v", wait)
	}
	if got, _ := h.incs.Get(ctx, incident.ID()); got.Status() == domain.IncidentApplying {
		t.Error("a denial applied the proposal")
	}
}

// The deadline covers the whole wait: a call approved once and not twice
// expires, keeping who approved it, and a late second approval
// authorises nothing.
func TestTwoPersonsExpiryKeepsTheFirstApproval(t *testing.T) {
	h := newHarness()
	_, task := incidentIn(t, h, domain.ApprovalsSnapshot{TTL: "2m", TwoPersons: &domain.TwoPersonsSnapshot{Namespaces: []string{"prod"}}}, "prod", "sev3")
	ctx := context.Background()
	runner := NewSerialRunner(h.deps)
	if _, err := runner.Decide(ctx, task.ID(), DecisionApprove, "", "tirso"); err != nil {
		t.Fatal(err)
	}
	h.decider.report = ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "not applied", SessionID: "sess-a"}}
	h.clock.now = h.clock.now.Add(3 * time.Minute)
	if _, err := runner.Decide(ctx, task.ID(), DecisionApprove, "", "oncall"); !domain.IsApprovalExpired(err) {
		t.Fatalf("late second approval: %v", err)
	}
	done, err := runner.ExpireApprovals(ctx)
	if err != nil || len(done) != 1 {
		t.Fatalf("expire: %v %+v", err, done)
	}
	after, _ := h.tasks.Get(ctx, task.ID())
	wait, _ := after.Approval()
	if wait.Outcome != domain.ApprovalExpired || !slices.Equal(approvers(wait), []string{"tirso"}) {
		t.Errorf("wait = %+v", wait)
	}
	if len(h.decider.requests) != 1 || h.decider.requests[0].Decision != DecisionDeny {
		t.Errorf("the expiry handed %+v", h.decider.requests)
	}
}

// An incident neither list names, and a task of no incident whatever the
// lists say, wait for one person.
func TestTwoPersonsLeavesOtherTasksAlone(t *testing.T) {
	h := newHarness()
	_, task := incidentIn(t, h, domain.ApprovalsSnapshot{TwoPersons: &domain.TwoPersonsSnapshot{Namespaces: []string{"prod"}, Severities: []string{"sev1"}}}, "dev", "sev3")
	if wait, _ := task.Approval(); wait.Needed() != 1 || wait.ApprovalsNeeded != 0 {
		t.Fatalf("dev sev3 wait = %+v", wait)
	}
	h.decider.report = ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "applied", SessionID: "sess-a"}}
	if _, err := NewApproveTask(h.deps).Execute(context.Background(), task.ID(), DecisionApprove, "", "tirso"); err != nil || len(h.decider.requests) != 1 {
		t.Fatalf("one approval: %v", err)
	}

	h = newHarness()
	_, plain := waitingWith(t, h, domain.ApprovalsSnapshot{TwoPersons: &domain.TwoPersonsSnapshot{Severities: []string{"sev1", "sev2", "sev3", "sev4"}}})
	if wait, _ := plain.Approval(); wait.Needed() != 1 {
		t.Fatalf("a task of no incident waits for %d", wait.Needed())
	}
	h.decider.report = ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done", SessionID: "sess-1"}}
	if _, err := NewApproveTask(h.deps).Execute(context.Background(), plain.ID(), DecisionApprove, "", ""); err != nil || len(h.decider.requests) != 1 {
		t.Fatalf("a task of no incident: %v", err)
	}
}

// A bot that asks for two persons after the wait began gets them; an
// incident that cannot be read is taken to need two.
func TestTwoPersonsSinceTheWaitBegan(t *testing.T) {
	h := newHarness()
	_, task := incidentIn(t, h, domain.ApprovalsSnapshot{}, "prod", "sev3")
	ctx := context.Background()
	fix, _ := h.bots.Get(ctx, task.BotID())
	governBot(t, h, fix, domain.ApprovalsSnapshot{TwoPersons: &domain.TwoPersonsSnapshot{Namespaces: []string{"prod"}}})
	res, err := NewApproveTask(h.deps).Execute(ctx, task.ID(), DecisionApprove, "", "tirso")
	if err != nil || len(h.decider.requests) != 0 || res.Task.Status() != domain.TaskWaitingApproval {
		t.Fatalf("raised wait: %v %d", err, len(h.decider.requests))
	}
	if wait, _ := res.Task.Approval(); wait.ApprovalsNeeded != 2 {
		t.Errorf("wait = %+v", wait)
	}
	delete(h.incs.incidents, task.IncidentID())
	updated, _ := h.bots.Get(ctx, task.BotID())
	if n := approvalsNeeded(ctx, h.deps, updated, res.Task); n != 2 {
		t.Errorf("an unreadable incident needs %d", n)
	}
}

// An autoApprove rule never decides a call that waits for two persons,
// and a policy is no person: approvers do not apply to it.
func TestPolicyAndTwoPersons(t *testing.T) {
	h := newHarness()
	_, task := incidentIn(t, h, domain.ApprovalsSnapshot{AutoApprove: []domain.ToolRuleSnapshot{{Tool: "local_write"}}, TwoPersons: &domain.TwoPersonsSnapshot{Namespaces: []string{"prod"}}}, "prod", "sev3")
	if task.Status() != domain.TaskWaitingApproval || len(h.decider.requests) != 0 {
		t.Fatalf("autoApprove decided a two-person call: %s", task.Status())
	}

	h = newHarness()
	bot := governBot(t, h, enabledBot(t, h), domain.ApprovalsSnapshot{Approvers: []string{"tirso"}, AutoApprove: []domain.ToolRuleSnapshot{{Tool: "local_exec"}}})
	submitted := submit(t, h, bot, "auto").Task
	h.runtime.report = pendingReport("sess-1")
	h.decider.report = ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done", SessionID: "sess-1"}}
	res, err := NewRunTask(h.deps).Execute(context.Background(), submitted.ID())
	if err != nil || len(h.decider.requests) != 1 {
		t.Fatalf("autoApprove on a bot with approvers: %v %s", err, res.Task.Status())
	}
	if decided, _ := lastOf(h, domain.EventTaskDecided); decided.Payload["decidedBy"] != PolicyActor {
		t.Errorf("decided = %+v", decided.Payload)
	}
}

// An incident an Alertmanager group opens keeps its namespace label, and
// the opened event says it; an incident opened without one takes the
// namespace of a later alert of its group.
func TestAnIncidentKeepsItsAlertsNamespace(t *testing.T) {
	h := newHarness()
	bot := alertmanagerBot(t, h, "")
	first := fireAlerts(t, h, bot, alertmanagerBody(t, "firing"))
	if first.Incident == nil || first.Incident.Namespace() != "shop" {
		t.Fatalf("incident = %+v", first.Incident)
	}
	if opened, ok := lastOf(h, domain.EventIncidentOpened); !ok || opened.Payload["namespace"] != "shop" {
		t.Errorf("opened = %+v", opened.Payload)
	}

	h = newHarness()
	triage := triageBot(t, h, "triage", "")
	ctx := context.Background()
	opened, err := NewOpenIncident(h.deps).Execute(ctx, alert(triage, "fp-a"))
	if err != nil || opened.Incident.Namespace() != "" {
		t.Fatalf("manual: %v %q", err, opened.Incident.Namespace())
	}
	later := alert(triage, "fp-b")
	later.Namespace = "prod"
	again, err := NewOpenIncident(h.deps).Execute(ctx, later)
	if err != nil || again.Incident.Namespace() != "prod" {
		t.Fatalf("later: %v %+v", err, again.Incident)
	}
	if stored, _ := h.incs.Get(ctx, opened.Incident.ID()); stored.Namespace() != "prod" {
		t.Errorf("stored namespace = %q", stored.Namespace())
	}
	bad := alert(triage, "fp-c")
	bad.Namespace, bad.GroupKey = "a b", "other"
	if _, err := NewOpenIncident(h.deps).Execute(ctx, bad); err == nil {
		t.Error("a namespace with a space opened an incident")
	}
}
