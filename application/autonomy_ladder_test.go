package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

const ladderRunbook, ladderNamespace = "KubePodCrashLooping", "shop"

// ladderTeam is the sre team with a ladder on its apply trigger: triage,
// fix at act-within-policy applying at act-with-approval, verify.
func ladderTeam(t *testing.T, h *harness, thresholds *domain.AutonomyThresholdsSnapshot, approvals domain.ApprovalsSnapshot) domain.Bot {
	t.Helper()
	triage := teamBot(t, h, "triage", []string{"sre"}, func(d *BotDefinition) {
		d.Triggers = []domain.TriggerSnapshot{{Type: "manual"}, {Type: "incident"}}
	})
	teamBot(t, h, "fix", []string{"sre"}, func(d *BotDefinition) {
		d.Level = "act-within-policy"
		d.AllowedTools = []string{"local_read", "local_list", "patch", "scale"}
		d.Approvals = approvals
		d.Triggers = []domain.TriggerSnapshot{
			{Type: "incident", Name: "fix", Phase: "fix", Level: "propose", Tools: []string{"local_read"}},
			{Type: "incident", Name: "apply", Phase: "apply", Level: "act-with-approval", Tools: []string{"local_read", "local_list", "patch", "scale"},
				Ladder: &domain.LadderSnapshot{Runbooks: []domain.LadderRunbookSnapshot{{Name: ladderRunbook, Tool: "patch", Namespaces: []string{ladderNamespace}}}, Thresholds: thresholds}},
		}
	})
	teamBot(t, h, "verify", []string{"sre"}, func(d *BotDefinition) {
		d.Triggers = []domain.TriggerSnapshot{{Type: "incident", Phase: "verify"}}
	})
	h.log.events, h.log.audits = nil, nil
	return triage
}

func shopAlert(bot domain.Bot, severity, namespace string) OpenIncidentCommand {
	command := alert(bot, "fp-a")
	command.Severity, command.Namespace = severity, namespace
	return command
}

func ladderProposed(t *testing.T, h *harness, command OpenIncidentCommand) domain.Incident {
	t.Helper()
	opened, err := NewOpenIncident(h.deps).Execute(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	return throughFix(t, h, opened.Incident.ID())
}

func decideApply(t *testing.T, h *harness, incident domain.Incident, decision, actor string) {
	t.Helper()
	waitingApply(t, h, incident)
	h.decider.report = completes("applied", 0.01)
	if _, err := NewApproveTask(h.deps).Execute(context.Background(), incident.CurrentTaskID(), decision, "", actor); err != nil {
		t.Fatal(err)
	}
}

func runCurrent(t *testing.T, h *harness, id domain.IncidentID, report ExecutionReport) domain.Incident {
	t.Helper()
	ctx := context.Background()
	incident, _ := h.incs.Get(ctx, id)
	h.runtime.report = report
	if _, err := NewRunTask(h.deps).Execute(ctx, incident.CurrentTaskID()); err != nil {
		t.Fatal(err)
	}
	incident, _ = h.incs.Get(ctx, id)
	return incident
}

func autonomyOf(t *testing.T, h *harness, incident domain.Incident) domain.RunbookAutonomy {
	t.Helper()
	fix, err := h.bots.Get(context.Background(), domain.BotID("fix"))
	if err != nil {
		t.Fatal(err)
	}
	return autonomyByName(t, h, fix.TenantID(), fix.ID())
}

func autonomyByName(t *testing.T, h *harness, tenant domain.TenantID, bot domain.BotID) domain.RunbookAutonomy {
	t.Helper()
	record, err := h.autos.Get(context.Background(), domain.AutonomyIDOf(tenant, bot, ladderRunbook, ladderNamespace))
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func mark(t *testing.T, h *harness, id domain.IncidentID, verdict, actor string) (AutonomyView, error) {
	t.Helper()
	return NewMarkDiagnosis(h.deps).Execute(context.Background(), id, verdict, actor)
}

// D13: each reopened cycle of one incident counts, and a run of twenty
// cycles inside the reopen window counts twenty.
func TestLadderCountsEachApprovedAndVerifiedCycle(t *testing.T) {
	h := newHarness()
	triage := ladderTeam(t, h, nil, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	command := shopAlert(triage, "sev2", ladderNamespace)
	incident := ladderProposed(t, h, command)
	for cycle := 1; cycle <= 20; cycle++ {
		if incident.Cycle() != cycle {
			t.Fatalf("cycle = %d, want %d", incident.Cycle(), cycle)
		}
		if _, err := mark(t, h, incident.ID(), DiagnosisCorrect, "tirso"); err != nil {
			t.Fatal(err)
		}
		decideApply(t, h, incident, DecisionApprove, "tirso")
		incident = runCurrent(t, h, incident.ID(), completes("VERDICT: green", 0.01))
		if incident.Status() != domain.IncidentVerifying {
			t.Fatalf("status %s", incident.Status())
		}
		if cycle == 20 {
			break
		}
		if _, err := NewResolveIncident(h.deps).Execute(ctx, incident.ID(), "green"); err != nil {
			t.Fatal(err)
		}
		h.clock.now = h.clock.now.Add(time.Minute)
		reopened, err := NewOpenIncident(h.deps).Execute(ctx, command)
		if err != nil {
			t.Fatal(err)
		}
		incident = throughFix(t, h, reopened.Incident.ID())
	}
	record := autonomyOf(t, h, incident)
	if got := record.Totals(); got.Diagnoses != 20 || got.Proposals != 20 || got.Applies != 20 {
		t.Fatalf("totals = %+v", got)
	}
	if record.Step() != domain.AutonomyActWithinPolicy {
		t.Fatalf("step %s after 5/10/20", record.Step())
	}
	if !strings.Contains(strings.Join(auditActions(h), " "), "autonomy.changed") {
		t.Error("the promotion was not audited")
	}
}

func auditActions(h *harness) []string {
	var out []string
	for _, entry := range h.log.audits {
		out = append(out, entry.Action)
	}
	return out
}

// One cycle never counts twice: a repeated signal, and a verify delivered
// again, change nothing.
func TestLadderOneCycleNeverCountsTwice(t *testing.T) {
	h := newHarness()
	triage := ladderTeam(t, h, nil, domain.ApprovalsSnapshot{})
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	decideApply(t, h, incident, DecisionApprove, "tirso")
	incident = runCurrent(t, h, incident.ID(), completes("VERDICT: green", 0.01))
	keeper := newLadderKeeper(h.deps)
	ctx := context.Background()
	keeper.signal(ctx, incident, incident.Cycle(), domain.SignalProposalApproved, "tirso")
	keeper.signal(ctx, incident, incident.Cycle(), domain.SignalApplyVerified, "")
	if got := autonomyOf(t, h, incident).Totals(); got.Proposals != 1 || got.Applies != 1 {
		t.Fatalf("totals = %+v", got)
	}
}

// Signals are a person's: a policy decider, an unnamed decider and an
// expired wait count nothing; a verify of a cycle nobody approved counts
// nothing; a green that was no verdict counts nothing.
func TestLadderCountsOnlyAPerson(t *testing.T) {
	h := newHarness()
	triage := ladderTeam(t, h, nil, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	waiting := waitingApply(t, h, incident)
	advancer := newIncidentAdvancer(h.deps)
	h.decider.report = completes("applied", 0.01)
	advancer.taskDecided(ctx, waiting, domain.ApprovalApproved, "c-apply", "patch", "policy:autoApprove", false)
	if _, err := h.autos.Get(ctx, domain.AutonomyIDOf("default", "fix", ladderRunbook, ladderNamespace)); !domain.IsNotFound(err) {
		t.Fatalf("a policy's approval reached the ladder: %v", err)
	}
	moved, _ := h.incs.Get(ctx, incident.ID())
	if moved.Status() != domain.IncidentApplying {
		t.Fatalf("status %s", moved.Status())
	}

	h2 := newHarness()
	triage2 := ladderTeam(t, h2, nil, domain.ApprovalsSnapshot{})
	other := ladderProposed(t, h2, shopAlert(triage2, "sev2", ladderNamespace))
	decideApply(t, h2, other, DecisionApprove, "")
	other = runCurrent(t, h2, other.ID(), completes("nothing to report", 0.01))
	if len(h2.autos.records) != 0 {
		t.Fatalf("an unnamed approval or no verdict reached the ladder: %+v", h2.autos.records)
	}
	keeper := newLadderKeeper(h2.deps)
	keeper.signal(ctx, other, other.Cycle(), domain.SignalApplyVerified, "")
	if len(h2.autos.records) != 0 {
		t.Fatal("a green of a cycle nobody approved counted")
	}
}

func TestLadderLowersOnADeny(t *testing.T) {
	h := newHarness()
	triage := ladderTeam(t, h, nil, domain.ApprovalsSnapshot{})
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	decideApply(t, h, incident, DecisionDeny, "tirso")
	record := autonomyOf(t, h, incident)
	if record.Step() != domain.AutonomyPropose {
		t.Fatalf("step %s", record.Step())
	}
	history := record.History()
	if len(history) != 1 || history[0].Signal != "apply-denied" || history[0].From != "act-with-approval" || history[0].To != "propose" || history[0].Actor != "tirso" {
		t.Fatalf("history = %+v", history)
	}
}

func TestLadderLowersTwoOnARedVerify(t *testing.T) {
	h := newHarness()
	triage := ladderTeam(t, h, nil, domain.ApprovalsSnapshot{})
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	decideApply(t, h, incident, DecisionApprove, "tirso")
	cycle := incident.Cycle()
	incident = runCurrent(t, h, incident.ID(), completes("VERDICT: red", 0.01))
	record := autonomyOf(t, h, incident)
	if record.Step() != domain.AutonomyObserve {
		t.Fatalf("step %s", record.Step())
	}
	if !record.Seen(incident.ID(), cycle, domain.SignalVerifyRed) {
		t.Fatal("the red verify is not in the history")
	}
}

// An apply that failed while Applying lowers two; one that failed while
// waiting on the card, with no write, lowers nothing.
func TestLadderLowersTwoOnAFailedApply(t *testing.T) {
	h := newHarness()
	triage := ladderTeam(t, h, nil, domain.ApprovalsSnapshot{})
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	waitingApply(t, h, incident)
	h.decider.report = ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "kube refused"}
	if _, err := NewApproveTask(h.deps).Execute(context.Background(), incident.CurrentTaskID(), DecisionApprove, "", "tirso"); err != nil {
		t.Fatal(err)
	}
	record := autonomyOf(t, h, incident)
	if record.Step() != domain.AutonomyObserve {
		t.Fatalf("step %s", record.Step())
	}

	h2 := newHarness()
	triage2 := ladderTeam(t, h2, nil, domain.ApprovalsSnapshot{})
	other := ladderProposed(t, h2, shopAlert(triage2, "sev2", ladderNamespace))
	runCurrent(t, h2, other.ID(), ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "model down"})
	if len(h2.autos.records) != 0 {
		t.Fatalf("a failure before any write reached the ladder: %+v", h2.autos.records)
	}
}

// topped is a team whose runbook already earned the top step, thresholds
// of one each.
func topped(t *testing.T, h *harness, approvals domain.ApprovalsSnapshot) domain.Bot {
	t.Helper()
	triage := ladderTeam(t, h, &domain.AutonomyThresholdsSnapshot{Diagnoses: 1, Proposals: 1, Applies: 1}, approvals)
	record, err := domain.NewRunbookAutonomy("default", "fix", ladderRunbook, ladderNamespace, domain.AutonomyActWithApproval, h.clock.now)
	if err != nil {
		t.Fatal(err)
	}
	thresholds, _ := domain.NewAutonomyThresholds(domain.AutonomyThresholdsSnapshot{Diagnoses: 1, Proposals: 1, Applies: 1})
	for _, signal := range []domain.AutonomySignal{domain.SignalDiagnosisCorrect, domain.SignalProposalApproved, domain.SignalApplyVerified} {
		if _, err := record.Record(signal, "inc-old", 1, "tirso", thresholds, h.clock.now); err != nil {
			t.Fatal(err)
		}
	}
	if record.Step() != domain.AutonomyActWithinPolicy {
		t.Fatalf("step %s", record.Step())
	}
	if err := h.autos.Save(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	return triage
}

func applyAudit(h *harness) (domain.AuditEntry, bool) {
	for n := len(h.log.audits) - 1; n >= 0; n-- {
		if h.log.audits[n].Action == "incident.apply" && h.log.audits[n].Outcome == domain.AuditOK {
			return h.log.audits[n], true
		}
	}
	return domain.AuditEntry{}, false
}

func TestLadderGrantsTheApplyAtTheTopStep(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	ctx := context.Background()
	task, _ := h.tasks.Get(ctx, incident.CurrentTaskID())
	pinned, ok := task.Pinned()
	if !ok || pinned.Level() != domain.AutonomyActWithinPolicy || !pinned.Unattended() || strings.Join(toolNames(pinned), ",") != "local_list,local_read,patch" {
		t.Fatalf("pinned = %+v %v", pinned, ok)
	}
	audit, _ := applyAudit(h)
	if audit.Details["ladder"] != "act-within-policy" || audit.Details["level"] != "act-within-policy" {
		t.Fatalf("audit = %+v", audit)
	}
	fix, _ := h.bots.Get(ctx, "fix")
	policy, gone, err := fix.PolicyOf(task)
	if err != nil || gone || !policy.Unattended() {
		t.Fatalf("policy %+v %v %v", policy, gone, err)
	}
	if rules := policy.Rules().Snapshot().Allow; len(rules) != 1 || rules[0].Tool != "patch" || rules[0].Arguments["namespace"] != ladderNamespace {
		t.Fatalf("rules = %+v", rules)
	}
	moved := runCurrent(t, h, incident.ID(), completes("patched", 0.01))
	if moved.Status() != domain.IncidentVerifying {
		t.Fatalf("status %s", moved.Status())
	}
	if len(h.runtime.requests) == 0 || !h.runtime.requests[len(h.runtime.requests)-1].Unattended {
		t.Fatal("the run was not unattended")
	}
	moved = runCurrent(t, h, incident.ID(), completes("VERDICT: green", 0.01))
	if got := autonomyOf(t, h, moved).Totals(); got.Applies != 1 {
		t.Fatalf("an unattended green counted: %+v", got)
	}
}

func toolNames(policy domain.ExecutionPolicy) []string { return policy.Tools().Names() }

func TestLadderLowersTwoWhenActingAloneFails(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	runCurrent(t, h, incident.ID(), ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "kube refused"})
	if step := autonomyOf(t, h, incident).Step(); step != domain.AutonomyPropose {
		t.Fatalf("step %s", step)
	}
}

func cardReason(t *testing.T, h *harness, command OpenIncidentCommand, want string) {
	t.Helper()
	incident := ladderProposed(t, h, command)
	task, _ := h.tasks.Get(context.Background(), incident.CurrentTaskID())
	if pinned, ok := task.Pinned(); !ok || pinned.Level() != domain.AutonomyActWithApproval || pinned.Unattended() {
		t.Fatalf("pinned = %+v %v", pinned, ok)
	}
	audit, _ := applyAudit(h)
	if !strings.Contains(audit.Details["ladder"], want) {
		t.Fatalf("audit = %+v, want %q", audit, want)
	}
}

func TestLadderKeepsTheCard(t *testing.T) {
	prod := domain.ApprovalsSnapshot{Approvers: []string{"tirso", "oncall"}, TwoPersons: &domain.TwoPersonsSnapshot{Namespaces: []string{ladderNamespace}}}
	t.Run("sev1 in production", func(t *testing.T) {
		h := newHarness()
		cardReason(t, h, shopAlert(topped(t, h, prod), "sev1", ladderNamespace), "sev1 in production")
	})
	t.Run("two persons", func(t *testing.T) {
		h := newHarness()
		cardReason(t, h, shopAlert(topped(t, h, prod), "sev3", ladderNamespace), "two persons")
	})
	t.Run("outside the bounds", func(t *testing.T) {
		h := newHarness()
		cardReason(t, h, shopAlert(topped(t, h, domain.ApprovalsSnapshot{}), "sev3", "other"), "outside the bounds")
	})
	t.Run("not earned", func(t *testing.T) {
		h := newHarness()
		cardReason(t, h, shopAlert(ladderTeam(t, h, nil, domain.ApprovalsSnapshot{}), "sev3", ladderNamespace), "stands on act-with-approval")
	})
	t.Run("a record that cannot be read", func(t *testing.T) {
		h := newHarness()
		triage := topped(t, h, domain.ApprovalsSnapshot{})
		h.autos.fail = errBoom
		cardReason(t, h, shopAlert(triage, "sev2", ladderNamespace), "could not be read")
	})
	t.Run("another alert", func(t *testing.T) {
		h := newHarness()
		triage := topped(t, h, domain.ApprovalsSnapshot{})
		command := shopAlert(triage, "sev2", ladderNamespace)
		command.Title = "Other in shop"
		command.GroupKey = "other"
		cardReason(t, h, command, "not on the ladder")
	})
}

// A bot with no ladder runs as before: no record, no grant.
func TestNoLadderChangesNothing(t *testing.T) {
	h := newHarness()
	triage := brigade(t, h, domain.ApprovalsSnapshot{})
	incident := proposed(t, h, triage)
	task, _ := h.tasks.Get(context.Background(), incident.CurrentTaskID())
	pinned, ok := task.Pinned()
	if ok && pinned.Unattended() {
		t.Fatal("no ladder, and the apply was granted")
	}
	decideApply(t, h, incident, DecisionApprove, "tirso")
	if len(h.autos.records) != 0 {
		t.Fatalf("records = %+v", h.autos.records)
	}
	if audit, ok := applyAudit(h); ok && audit.Details["ladder"] != "" {
		t.Fatalf("audit = %+v", audit)
	}
	report, err := NewGetIncident(h.deps).Execute(context.Background(), incident.ID())
	if err != nil || report.Autonomy != nil {
		t.Fatalf("report %+v %v", report.Autonomy, err)
	}
	if _, err := mark(t, h, incident.ID(), DiagnosisCorrect, "tirso"); err == nil {
		t.Fatal("a mark on an incident with no ladder passed")
	}
}

func TestMarkDiagnosis(t *testing.T) {
	h := newHarness()
	triage := ladderTeam(t, h, nil, domain.ApprovalsSnapshot{Approvers: []string{"tirso"}})
	ctx := context.Background()
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	if _, err := mark(t, h, incident.ID(), "odd", "tirso"); err == nil {
		t.Fatal("an unknown verdict passed")
	}
	for _, actor := range []string{"", "policy:autoApprove", "someone"} {
		if _, err := mark(t, h, incident.ID(), DiagnosisCorrect, actor); err == nil {
			t.Fatalf("%q marked", actor)
		}
	}
	view, err := mark(t, h, incident.ID(), DiagnosisCorrect, "cli:tirso")
	if err != nil {
		t.Fatal(err)
	}
	if view.DiagnosisMark != "diagnosis-correct" || view.Autonomy.Totals().Diagnoses != 1 || !view.Saved {
		t.Fatalf("view = %+v", view.Autonomy.Snapshot())
	}
	if view, err = mark(t, h, incident.ID(), DiagnosisCorrect, "tirso"); err != nil || view.Autonomy.Totals().Diagnoses != 1 {
		t.Fatalf("repeat mark: %v %+v", err, view.Autonomy.Totals())
	}
	if _, err := mark(t, h, incident.ID(), DiagnosisWrong, "tirso"); err == nil {
		t.Fatal("a second, different mark passed")
	}
	report, err := NewGetIncident(h.deps).Execute(ctx, incident.ID())
	if err != nil || report.Autonomy == nil || report.Autonomy.DiagnosisMark != "diagnosis-correct" {
		t.Fatalf("report: %v %+v", err, report.Autonomy)
	}
	list, err := NewListAutonomy(h.deps).Execute(ctx, AutonomyQuery{Runbook: ladderRunbook})
	if err != nil || len(list) != 1 || list[0].Thresholds == nil {
		t.Fatalf("list: %v %+v", err, list)
	}
	got, err := NewGetAutonomy(h.deps).Execute(ctx, list[0].Autonomy.ID())
	if err != nil || got.Autonomy.Totals().Diagnoses != 1 {
		t.Fatalf("get: %v", err)
	}
	if none, _ := NewListAutonomy(h.deps).Execute(ctx, AutonomyQuery{Runbook: "other"}); len(none) != 0 {
		t.Fatalf("filter let through %+v", none)
	}
}

// A diagnosis of an earlier cycle is not this cycle's: Open, it may not
// be marked.
func TestMarkDiagnosisNeedsThisCyclesDiagnosis(t *testing.T) {
	h := newHarness()
	triage := ladderTeam(t, h, nil, domain.ApprovalsSnapshot{})
	opened, err := NewOpenIncident(h.deps).Execute(context.Background(), shopAlert(triage, "sev2", ladderNamespace))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mark(t, h, opened.Incident.ID(), DiagnosisCorrect, "tirso"); err == nil {
		t.Fatal("marked before any diagnosis")
	}
}

func TestMarkDiagnosisOnADaemon(t *testing.T) {
	h := newHarness()
	h.deps.Daemon = true
	triage := ladderTeam(t, h, nil, domain.ApprovalsSnapshot{})
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	if _, err := mark(t, h, incident.ID(), DiagnosisCorrect, h.deps.Actor); err == nil {
		t.Fatal("the control plane marked as itself")
	}
}

// A store that fails costs a count, never the apply.
func TestLadderStoreFailureIsAuditedAndCarriedOn(t *testing.T) {
	h := newHarness()
	triage := ladderTeam(t, h, nil, domain.ApprovalsSnapshot{})
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	h.autos.fail = errBoom
	decideApply(t, h, incident, DecisionApprove, "tirso")
	if !strings.Contains(strings.Join(auditActions(h), " "), "autonomy.signal") {
		t.Errorf("not audited: %v", auditActions(h))
	}
	moved, _ := h.incs.Get(context.Background(), incident.ID())
	if moved.Status() != domain.IncidentVerifying {
		t.Fatalf("status %s", moved.Status())
	}
}
