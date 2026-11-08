package application

// Repros for the review of underpass-ai/axlr-bots#143 (fff6ed4). Each test
// asserts the behaviour design §8.3 / D13 / D14 / the ADR promise: a FAIL
// is a confirmed defect.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

func lastUnattended(h *harness) bool {
	return len(h.runtime.requests) > 0 && h.runtime.requests[len(h.runtime.requests)-1].Unattended
}

func otherIncident(triage domain.Bot, group, fp string) OpenIncidentCommand {
	c := shopAlert(triage, "sev2", ladderNamespace)
	c.GroupKey, c.Fingerprints = group, []string{fp}
	return c
}

// #1 grant pinned at launch survives a lowering of its own runbook.
func TestR01_GrantRunsAloneAfterItsRunbookWasLowered(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	a := ladderProposed(t, h, otherIncident(triage, "group-a", "fp-a")) // granted, still Pending: node busy or away
	b := ladderProposed(t, h, otherIncident(triage, "group-b", "fp-b"))
	runCurrent(t, h, b.ID(), completes("patched", 0.01))
	runCurrent(t, h, b.ID(), completes("VERDICT: red", 0.01))
	if step := autonomyOf(t, h, b).Step(); step == domain.AutonomyActWithinPolicy {
		t.Fatalf("setup: the red did not lower: %s", step)
	}
	runCurrent(t, h, a.ID(), completes("patched", 0.01))
	if lastUnattended(h) {
		t.Errorf("task of incident A ran with no card after its runbook was lowered to %s", autonomyOf(t, h, b).Step())
	}
}

// #2 the incident no longer stands on the task; approve would be refused
// (standsOn), an unattended run is not.
func TestR02_GrantRunsAloneOnAResolvedIncident(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	if _, err := NewResolveIncident(h.deps).Execute(context.Background(), incident.ID(), "alert went green"); err != nil {
		t.Fatal(err)
	}
	before := len(h.runtime.requests)
	h.runtime.report = completes("patched", 0.01)
	if _, err := NewRunTask(h.deps).Execute(context.Background(), incident.CurrentTaskID()); err != nil {
		t.Logf("run refused: %v", err)
	}
	if len(h.runtime.requests) > before && lastUnattended(h) {
		t.Errorf("a write with no card ran for an incident already Resolved")
	}
}

func fixTriggers(ladder *domain.LadderSnapshot, apply bool) []domain.TriggerSnapshot {
	triggers := []domain.TriggerSnapshot{{Type: "incident", Name: "fix", Phase: "fix", Level: "propose", Tools: []string{"local_read"}}}
	if apply {
		triggers = append(triggers, domain.TriggerSnapshot{Type: "incident", Name: "apply", Phase: "apply", Level: "act-with-approval", Tools: []string{"local_read", "local_list", "patch", "scale"}, Ladder: ladder})
	}
	return triggers
}

func editFix(t *testing.T, h *harness, patch BotPatch) {
	t.Helper()
	if _, err := NewUpdateBot(h.deps).Execute(context.Background(), "fix", patch); err != nil {
		t.Fatal(err)
	}
}

// #3 bot edits between launch and run.
func TestR03_BotEditsBetweenLaunchAndRun(t *testing.T) {
	for name, edit := range map[string]BotPatch{
		"ladder removed from the trigger":     {Triggers: ptr(fixTriggers(nil, true))},
		"apply trigger removed":               {Triggers: ptr(fixTriggers(nil, false))},
		"namespace taken out of bounds":       {Triggers: ptr(fixTriggers(&domain.LadderSnapshot{Runbooks: []domain.LadderRunbookSnapshot{{Name: ladderRunbook, Tool: "patch", Namespaces: []string{"elsewhere"}}}}, true))},
		"two persons added for the namespace": {Approvals: &domain.ApprovalsSnapshot{Approvers: []string{"a", "b"}, TwoPersons: &domain.TwoPersonsSnapshot{Namespaces: []string{ladderNamespace}}}},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness()
			triage := topped(t, h, domain.ApprovalsSnapshot{})
			incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
			editFix(t, h, edit)
			runCurrent(t, h, incident.ID(), completes("patched", 0.01))
			if lastUnattended(h) {
				t.Errorf("still unattended after: %s", name)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

// #4 a second namespace or two-persons trigger that appears on the incident
// between launch and run is not looked at either.
func TestR04_SecondNamespaceJoinsBetweenLaunchAndRun(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	command := shopAlert(triage, "sev2", ladderNamespace)
	incident := ladderProposed(t, h, command)
	second := command
	second.Namespace, second.Fingerprints = "payments", []string{"fp-b"}
	if _, err := NewOpenIncident(h.deps).Execute(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	moved, _ := h.incs.Get(context.Background(), incident.ID())
	if len(moved.Namespaces()) != 2 {
		t.Fatalf("setup: namespaces %v", moved.Namespaces())
	}
	runCurrent(t, h, incident.ID(), completes("patched", 0.01))
	if lastUnattended(h) {
		t.Errorf("unattended on an incident that now names %v: the unit is one namespace", moved.Namespaces())
	}
}

// #5 lowering lost: a namespace joined after the apply, so the unit is
// no longer computable and the red verify is dropped.
func TestR05_RedVerifyLostWhenASecondNamespaceJoined(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	command := shopAlert(triage, "sev2", ladderNamespace)
	incident := ladderProposed(t, h, command)
	runCurrent(t, h, incident.ID(), completes("patched", 0.01)) // acted alone, Verifying
	second := command
	second.Namespace, second.Fingerprints = "payments", []string{"fp-b"}
	if _, err := NewOpenIncident(h.deps).Execute(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	runCurrent(t, h, incident.ID(), completes("VERDICT: red", 0.01))
	if step := autonomyOf(t, h, incident).Step(); step == domain.AutonomyActWithinPolicy {
		t.Errorf("red verify after an apply that acted alone did not lower: still %s", step)
	}
}

// #6 lowering lost: the source resolved the group while the verify ran.
func TestR06_RedVerifyLostWhenIncidentResolvedFirst(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	runCurrent(t, h, incident.ID(), completes("patched", 0.01))
	if _, err := NewResolveIncident(h.deps).Execute(context.Background(), incident.ID(), "flapped green"); err != nil {
		t.Fatal(err)
	}
	runCurrent(t, h, incident.ID(), completes("VERDICT: red", 0.01))
	if step := autonomyOf(t, h, incident).Step(); step == domain.AutonomyActWithinPolicy {
		t.Errorf("red verdict of an apply that acted alone did not lower: still %s", step)
	}
}

// #8 ADR: "a lost signal costs a count, never an apply". A lost lowering
// costs the next apply its card.
func TestR08_StoreFailureLosesALoweringAndTheNextApplyActsAlone(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	a := ladderProposed(t, h, otherIncident(triage, "group-a", "fp-a"))
	runCurrent(t, h, a.ID(), completes("patched", 0.01))
	h.autos.fail = errBoom
	runCurrent(t, h, a.ID(), completes("VERDICT: red", 0.01))
	h.autos.fail = nil
	b := ladderProposed(t, h, otherIncident(triage, "group-b", "fp-b"))
	task, _ := h.tasks.Get(context.Background(), b.CurrentTaskID())
	if pinned, ok := task.Pinned(); ok && pinned.Unattended() {
		t.Errorf("the red was lost and the next apply was granted again")
	}
}

// #10 D14/§8.3 "nunca en prod sev1": a bot with no twoPersons has no
// production; checkLadder does not ask for one.
func TestR10_Sev1ActsAloneWhenTwoPersonsIsNotConfigured(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	incident := ladderProposed(t, h, shopAlert(triage, "sev1", ladderNamespace))
	task, _ := h.tasks.Get(context.Background(), incident.CurrentTaskID())
	if pinned, ok := task.Pinned(); ok && pinned.Unattended() {
		t.Errorf("a sev1 in %s was granted: the ladder has no notion of production without twoPersons", ladderNamespace)
	}
}

// #12 forged name: alertname holding " in " inherits the history of the
// runbook before the cut. Real payload.
func TestR12_AlertnameAliasInheritsAnotherRunbook(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	body := `{"version":"4","groupKey":"k","status":"firing","groupLabels":{"alertname":"KubePodCrashLooping in disguise","namespace":"shop"},"alerts":[{"status":"firing","fingerprint":"f1","labels":{}}]}`
	payload, err := domain.ParseAlertmanagerPayload([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	command := OpenIncidentCommand{Tenant: "default", BotID: triage.ID().String(), GroupKey: payload.GroupKey, Source: "alertmanager", Title: payload.Title(), Severity: "sev3", Namespace: "shop", Fingerprints: []string{"f1"}}
	if name, _ := domain.RunbookOf("alertmanager", payload.Title()); name == ladderRunbook {
		t.Errorf("alert %q parsed as runbook %s", "KubePodCrashLooping in disguise", name)
	}
	incident := ladderProposed(t, h, command)
	task, _ := h.tasks.Get(context.Background(), incident.CurrentTaskID())
	if pinned, ok := task.Pinned(); ok && pinned.Unattended() {
		t.Errorf("a different alert was granted under runbook %s", ladderRunbook)
	}
}

// #13 a group of mixed alerts takes the first alert's name for the whole
// incident: Title() falls back to the first alert when no group/common
// label names it (group_by: [namespace]).
func TestR13_MixedGroupInheritsTheFirstAlertsRunbook(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	body := `{"version":"4","groupKey":"ns-shop","status":"firing","groupLabels":{"namespace":"shop"},"commonLabels":{"namespace":"shop"},"alerts":[
	 {"status":"firing","fingerprint":"f1","labels":{"alertname":"KubePodCrashLooping","namespace":"shop"}},
	 {"status":"firing","fingerprint":"f2","labels":{"alertname":"PaymentsDatabaseDown","namespace":"shop","severity":"critical"}}]}`
	payload, err := domain.ParseAlertmanagerPayload([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if payload.Title() != "KubePodCrashLooping in shop" {
		t.Fatalf("title %q", payload.Title())
	}
	command := OpenIncidentCommand{Tenant: "default", BotID: triage.ID().String(), GroupKey: payload.GroupKey, Source: "alertmanager", Title: payload.Title(), Severity: "sev3", Namespace: "shop", Fingerprints: []string{"f1", "f2"}}
	incident := ladderProposed(t, h, command)
	task, _ := h.tasks.Get(context.Background(), incident.CurrentTaskID())
	if pinned, ok := task.Pinned(); ok && pinned.Unattended() {
		t.Errorf("an incident of two different alerts (severity read %s, one of them critical) acted alone on the first alert's history", payload.Severity())
	}
}

// #14 history earned with one tool buys another: the record's id has no
// tool and the ladder's tool is an edit.
func TestR14_ChangingTheRunbooksToolInheritsTheHistory(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	editFix(t, h, BotPatch{Triggers: ptr(fixTriggers(&domain.LadderSnapshot{Runbooks: []domain.LadderRunbookSnapshot{{Name: ladderRunbook, Tool: "scale", Namespaces: []string{ladderNamespace}}}, Thresholds: &domain.AutonomyThresholdsSnapshot{Diagnoses: 1, Proposals: 1, Applies: 1}}, true))})
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	task, _ := h.tasks.Get(context.Background(), incident.CurrentTaskID())
	if pinned, ok := task.Pinned(); ok && pinned.Unattended() {
		t.Errorf("tool %v granted alone on history earned with patch", pinned.Tools().Names())
	}
}

// #17 a second call denied while Applying neither lowers nor moves.
func TestR17_DenialOfASecondCallWhileApplyingDoesNotLower(t *testing.T) {
	h := newHarness()
	triage := ladderTeam(t, h, nil, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	waitingApply(t, h, incident)
	h.decider.report = ExecutionReport{Outcome: domain.ExecutionDecisionRequired, Result: domain.Result{SessionID: "sess-a", PendingDecision: &domain.PendingDecision{SessionID: "sess-a", CallID: "c-second", Tool: "scale"}}}
	if _, err := NewApproveTask(h.deps).Execute(ctx, incident.CurrentTaskID(), DecisionApprove, "", "tirso"); err != nil {
		t.Fatal(err)
	}
	moved, _ := h.incs.Get(ctx, incident.ID())
	if moved.Status() != domain.IncidentApplying {
		t.Fatalf("setup: %s", moved.Status())
	}
	h.decider.report = completes("denied, stopped", 0.01)
	if _, err := NewApproveTask(h.deps).Execute(ctx, incident.CurrentTaskID(), DecisionDeny, "", "tirso"); err != nil {
		t.Fatal(err)
	}
	if step := autonomyOf(t, h, incident).Step(); step != domain.AutonomyPropose {
		t.Errorf("a person denied the write while Applying: step stays %s", step)
	}
	if autonomyOf(t, h, incident).Totals().Proposals != 1 {
		t.Fatalf("setup: %+v", autonomyOf(t, h, incident).Totals())
	}
}

// #18 approving the first call counts regardless of which tool it was;
// the runbook's tool is not compared.
func TestR18_ApprovingADifferentToolEarnsTheRunbook(t *testing.T) {
	h := newHarness()
	triage := ladderTeam(t, h, nil, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace)) // runbook's tool is patch
	h.runtime.report = ExecutionReport{Outcome: domain.ExecutionDecisionRequired, Result: domain.Result{SessionID: "sess-a", PendingDecision: &domain.PendingDecision{SessionID: "sess-a", CallID: "c-1", Tool: "scale"}}}
	if _, err := NewRunTask(h.deps).Execute(ctx, incident.CurrentTaskID()); err != nil {
		t.Fatal(err)
	}
	h.decider.report = completes("scaled", 0.01)
	if _, err := NewApproveTask(h.deps).Execute(ctx, incident.CurrentTaskID(), DecisionApprove, "", "tirso"); err != nil {
		t.Fatal(err)
	}
	record, err := h.autos.Get(ctx, domain.AutonomyIDOf("default", "fix", ladderRunbook, ladderNamespace))
	if err == nil && record.Totals().Proposals != 0 {
		t.Errorf("approving scale earned runbook whose one tool is patch: %+v", record.Totals())
	}
}

// resolvingRuntime resolves the incident while the run is in flight.
type resolvingRuntime struct {
	inner   RuntimeExecutor
	resolve func()
}

func (r resolvingRuntime) Execute(ctx context.Context, request ExecutionRequest) (ExecutionReport, error) {
	r.resolve()
	return r.inner.Execute(ctx, request)
}

// #7 as F1 leaves it: a run may only fail after a resolve if the resolve
// came while it ran.
func TestR07_FailedAloneApplyLostWhenIncidentResolvedDuringTheRun(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	deps := h.deps
	deps.Runtime = resolvingRuntime{inner: h.runtime, resolve: func() {
		if _, err := NewResolveIncident(h.deps).Execute(ctx, incident.ID(), "green"); err != nil {
			t.Fatal(err)
		}
	}}
	h.runtime.report = ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "kube refused halfway", Result: domain.Result{SessionID: "s", ToolCalls: []domain.ToolCall{{Name: "patch", CallID: "c", Status: "failed"}}}}
	if _, err := NewRunTask(deps).Execute(ctx, incident.CurrentTaskID()); err != nil {
		t.Fatal(err)
	}
	if step := autonomyOf(t, h, incident).Step(); step == domain.AutonomyActWithinPolicy {
		t.Errorf("a failed unattended apply did not lower: still %s", step)
	}
}

// #9 with F9 a ladder bot lists approvers, so an unnamed decision never
// gets to the task: what reaches the advancer unnamed still lowers.
func TestR09_ADenialLowersNamedOrNot(t *testing.T) {
	for name, by := range map[string]string{"unnamed": "", "daemon": "cli:gx10a"} {
		t.Run(name, func(t *testing.T) {
			h := newHarness()
			h.deps.Daemon, h.deps.Actor = true, "cli:gx10a"
			triage := ladderTeam(t, h, nil, domain.ApprovalsSnapshot{})
			ctx := context.Background()
			incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
			waiting := waitingApply(t, h, incident)
			newIncidentAdvancer(h.deps).taskDecided(ctx, waiting, decidedCall{outcome: domain.ApprovalDenied, callID: "c-apply", tool: "patch", effectiveTool: "patch", by: by})
			if step := autonomyOf(t, h, incident).Step(); step != domain.AutonomyPropose {
				t.Errorf("step %s", step)
			}
		})
	}
}

func TestExpiryDoesNotLower(t *testing.T) {
	h := newHarness()
	triage := ladderTeam(t, h, nil, domain.ApprovalsSnapshot{})
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	waiting := waitingApply(t, h, incident)
	newIncidentAdvancer(h.deps).taskDecided(context.Background(), waiting, decidedCall{outcome: domain.ApprovalExpired, callID: "c-apply", tool: "patch", effectiveTool: "patch"})
	if len(h.autos.records) != 0 {
		t.Errorf("an expiry reached the ladder: %+v", h.autos.records)
	}
}

// #11 with F9: a machine's name, or a registered bot's, is no person: it
// is refused at the door and earns nothing.
func TestR11_NonPersonNamesEarnNothing(t *testing.T) {
	for _, actor := range []string{"jev", "bot:triage", "triage", "Policy:autoApprove", "node:sre-rw", "trigger:apply"} {
		t.Run(actor, func(t *testing.T) {
			h := newHarness()
			triage := ladderTeam(t, h, nil, domain.ApprovalsSnapshot{})
			incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
			if _, err := mark(t, h, incident.ID(), DiagnosisCorrect, actor); err == nil {
				t.Errorf("%q marked a diagnosis", actor)
			}
			waitingApply(t, h, incident)
			if _, err := NewApproveTask(h.deps).Execute(context.Background(), incident.CurrentTaskID(), DecisionApprove, "", actor); err == nil {
				t.Errorf("%q approved", actor)
			}
			if len(h.autos.records) != 0 {
				if got := autonomyOf(t, h, incident).Totals(); got.Proposals != 0 {
					t.Errorf("%q earned: %+v", actor, got)
				}
			}
		})
	}
}

func TestR15_ANewBotWithTheSameIDStartsFromZero(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	if err := h.bots.Delete(context.Background(), "fix"); err != nil {
		t.Fatal(err)
	}
	thresholds := &domain.AutonomyThresholdsSnapshot{Diagnoses: 1, Proposals: 1, Applies: 1}
	teamBot(t, h, "fix", []string{"sre"}, func(d *BotDefinition) {
		d.Level = "act-within-policy"
		d.AllowedTools = []string{"local_read", "local_list", "patch", "scale"}
		d.Approvals = domain.ApprovalsSnapshot{Approvers: []string{"tirso"}}
		d.Triggers = fixTriggers(&domain.LadderSnapshot{Runbooks: []domain.LadderRunbookSnapshot{{Name: ladderRunbook, Tool: "patch", Namespaces: []string{ladderNamespace}}}, Thresholds: thresholds}, true)
	})
	if len(h.autos.records) != 0 {
		t.Fatalf("records kept: %+v", h.autos.records)
	}
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	if pinnedUnattended(t, h, incident) {
		t.Errorf("a brand new bot acted alone on its predecessor's history")
	}
}

func TestDeleteBotRemovesItsRecords(t *testing.T) {
	h := newHarness()
	topped(t, h, domain.ApprovalsSnapshot{})
	if _, err := NewDeleteBot(h.deps).Execute(context.Background(), "fix"); err != nil {
		t.Fatal(err)
	}
	if len(h.autos.records) != 0 {
		t.Fatalf("records kept: %+v", h.autos.records)
	}
}

// R19: a provider error before any call counts nothing and ends the entry.
func TestR19_NoToolCallCountsNothing(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	runCurrent(t, h, incident.ID(), ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "model provider 529", Result: domain.Result{SessionID: "s"}})
	record := autonomyOf(t, h, incident)
	if record.Step() != domain.AutonomyActWithinPolicy || len(record.Outstanding()) != 0 {
		t.Errorf("step %s outstanding %+v", record.Step(), record.Outstanding())
	}
}

// A run with no report of what it did may have written: it lowers.
func TestAFailureThatReportsNothingLowers(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	runCurrent(t, h, incident.ID(), ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "node lost"})
	if step := autonomyOf(t, h, incident).Step(); step != domain.AutonomyPropose {
		t.Errorf("step %s", step)
	}
}

// F6: while an apply has no recorded end the unit gets no grant, and a
// recorded green clears it.
func TestAnOutstandingApplyHoldsTheCard(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	a := ladderProposed(t, h, otherIncident(triage, "group-a", "fp-a"))
	if !pinnedUnattended(t, h, a) {
		t.Fatal("setup: not granted")
	}
	if got := autonomyOf(t, h, a).Outstanding(); len(got) != 1 {
		t.Fatalf("outstanding %+v", got)
	}
	b := ladderProposed(t, h, otherIncident(triage, "group-b", "fp-b"))
	if pinnedUnattended(t, h, b) {
		t.Error("granted while an apply has no recorded end")
	}
	runCurrent(t, h, a.ID(), completes("patched", 0.01))
	runCurrent(t, h, a.ID(), completes("VERDICT: green", 0.01))
	if got := autonomyOf(t, h, a).Outstanding(); len(got) != 0 {
		t.Fatalf("a green left %+v", got)
	}
	c := ladderProposed(t, h, otherIncident(triage, "group-c", "fp-c"))
	if !pinnedUnattended(t, h, c) {
		t.Error("no grant after the green")
	}
}

func TestAnApprovedApplyIsOutstandingUntilItsGreen(t *testing.T) {
	h := newHarness()
	triage := ladderTeam(t, h, nil, domain.ApprovalsSnapshot{})
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	decideApply(t, h, incident, DecisionApprove, "tirso")
	if got := autonomyOf(t, h, incident).Outstanding(); len(got) != 1 {
		t.Fatalf("outstanding %+v", got)
	}
	runCurrent(t, h, incident.ID(), completes("VERDICT: green", 0.01))
	if got := autonomyOf(t, h, incident).Outstanding(); len(got) != 0 {
		t.Fatalf("after the green %+v", got)
	}
}

// A grant whose outstanding entry cannot be written is no grant.
func TestNoGrantWithoutItsOutstandingEntry(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	h.autos.failSave = errBoom
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	if pinnedUnattended(t, h, incident) {
		t.Error("granted with nothing recorded")
	}
}

// F1: a person stops a granted task before it runs.
func TestAPersonLowersARunbookAndTheGrantIsTakenBack(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	id := autonomyOf(t, h, incident).ID()
	if _, err := NewLowerAutonomy(h.deps).Execute(ctx, id, "tirso", "stop"); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"", "someone", "policy:x", "triage"} {
		if _, err := NewLowerAutonomy(h.deps).Execute(ctx, id, actor, ""); err == nil {
			t.Errorf("%q lowered", actor)
		}
	}
	runCurrent(t, h, incident.ID(), completes("patched", 0.01))
	if lastUnattended(h) {
		t.Error("ran alone after a person lowered it")
	}
	task, _ := h.tasks.Get(ctx, incident.CurrentTaskID())
	_ = task
	if !strings.Contains(strings.Join(auditLadders(h), " "), "taken back") {
		t.Errorf("not audited: %v", auditLadders(h))
	}
}

func auditLadders(h *harness) []string {
	var out []string
	for _, entry := range h.log.audits {
		if entry.Details["ladder"] != "" {
			out = append(out, entry.Details["ladder"])
		}
	}
	return out
}

// Node path: a grant lowered is the card; a resolved incident no run.
func TestR20_NodeAssignmentIsTakenBackOrNotGiven(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := ladderProposed(t, h, otherIncident(triage, "group-a", "fp-a"))
	if _, err := NewLowerAutonomy(h.deps).Execute(ctx, autonomyOf(t, h, incident).ID(), "tirso", "stop"); err != nil {
		t.Fatal(err)
	}
	node := onlineNode(t, h, "web-1")
	assignment, found, err := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	if err != nil || !found || assignment.Run == nil || assignment.Run.Unattended || assignment.Run.Level != domain.AutonomyActWithApproval {
		t.Fatalf("assignment %+v %v %v", assignment.Run, found, err)
	}

	h2 := newHarness()
	triage2 := topped(t, h2, domain.ApprovalsSnapshot{})
	gone := ladderProposed(t, h2, otherIncident(triage2, "group-a", "fp-a"))
	if _, err := NewResolveIncident(h2.deps).Execute(ctx, gone.ID(), "green"); err != nil {
		t.Fatal(err)
	}
	node2 := onlineNode(t, h2, "web-1")
	if _, found, err := NewNextAssignment(h2.deps).Execute(ctx, node2.ID()); err != nil || found {
		t.Fatalf("a resolved incident's task was assigned: %v %v", found, err)
	}
	task, _ := h2.tasks.Get(ctx, gone.CurrentTaskID())
	if task.Status() != domain.TaskCancelled {
		t.Errorf("task %s", task.Status())
	}
}

// F11: more than one write lowers two, and the budget is pinned low.
func TestARunActingAloneIsHeldToOneWrite(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	report := completes("patched twice", 0.01)
	report.Result.ToolCalls = []domain.ToolCall{{Name: "local_read", Status: "completed"}, {Name: "axlr_call_tool", Status: "completed"}, {Name: "patch", Status: "completed"}}
	runCurrent(t, h, incident.ID(), report)
	request := h.runtime.requests[len(h.runtime.requests)-1]
	if got := request.Budgets.MaxToolCallsPerTask(); got != domain.LadderMaxToolCalls {
		t.Errorf("max tool calls %d", got)
	}
	record := autonomyOf(t, h, incident)
	if record.Step() != domain.AutonomyPropose || !record.Seen(incident.ID(), incident.Cycle(), domain.SignalApplyOverrun) {
		t.Errorf("step %s history %+v", record.Step(), record.History())
	}
}

// F7: a write denied while Applying sends the incident back.
func TestADenialWhileApplyingNeverReachesVerifying(t *testing.T) {
	h := newHarness()
	triage := ladderTeam(t, h, nil, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	waitingApply(t, h, incident)
	h.decider.report = ExecutionReport{Outcome: domain.ExecutionDecisionRequired, Result: domain.Result{SessionID: "sess-a", PendingDecision: &domain.PendingDecision{SessionID: "sess-a", CallID: "c-second", Tool: "scale"}}}
	if _, err := NewApproveTask(h.deps).Execute(ctx, incident.CurrentTaskID(), DecisionApprove, "", "tirso"); err != nil {
		t.Fatal(err)
	}
	h.decider.report = completes("stopped", 0.01)
	if _, err := NewApproveTask(h.deps).Execute(ctx, incident.CurrentTaskID(), DecisionDeny, "", "tirso"); err != nil {
		t.Fatal(err)
	}
	moved, _ := h.incs.Get(ctx, incident.ID())
	if moved.Status() != domain.IncidentOpen || moved.Cycle() != incident.Cycle()+1 {
		t.Fatalf("status %s cycle %d", moved.Status(), moved.Cycle())
	}
}

// F2: a mixed delivery, in or after the opening, is never granted.
func TestAMixedGroupThroughAlertmanagerKeepsTheCard(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	mixed, _ := domain.ParseAlertmanagerPayload([]byte(mixedBody))
	if mixed.Uniform() {
		t.Fatal("setup: uniform")
	}
	opened, err := NewOpenIncident(h.deps).open(ctx, opening{bot: triage, trigger: mustIncidentTrigger(t, triage), source: AlertmanagerSource, groupKey: "m", title: mixed.Title(), severity: mixed.Severity(), namespaces: mixed.Namespaces(), alerts: alertGroupOf(mixed), fingerprints: []string{"f1", "f2"}})
	if err != nil {
		t.Fatal(err)
	}
	incident := throughFix(t, h, opened.Incident.ID())
	if pinnedUnattended(t, h, incident) || incident.AlertGroup() != domain.AlertGroupMixed {
		t.Errorf("mixed: granted or %s", incident.AlertGroup())
	}
}

const mixedBody = `{"version":"4","groupKey":"m","status":"firing","groupLabels":{"namespace":"shop"},"alerts":[
 {"status":"firing","fingerprint":"f1","labels":{"alertname":"KubePodCrashLooping","namespace":"shop"}},
 {"status":"firing","fingerprint":"f2","labels":{"alertname":"PaymentsDatabaseDown","namespace":"shop","severity":"critical"}}]}`

func mustIncidentTrigger(t *testing.T, bot domain.Bot) domain.Trigger {
	t.Helper()
	trigger, ok := bot.Triggers().Incident(domain.IncidentPhaseTriage)
	if !ok {
		t.Fatal("no incident trigger")
	}
	return trigger
}

// Re-attach through the idempotency key: the audit tells the grant the
// task really carries.
func TestAReattachAuditsTheGrantTheTaskCarries(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	stored, _ := h.incs.Get(ctx, incident.ID())
	snapshot := stored.Snapshot()
	snapshot.Tasks, snapshot.TaskIDs = snapshot.Tasks[:len(snapshot.Tasks)-1], nil
	rolled, err := domain.RestoreIncident(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	// Now not granted: the runbook was lowered between the crash and the re-attach.
	record := autonomyOf(t, h, incident)
	if err := record.Lower("tirso", "x", h.clock.now); err != nil {
		t.Fatal(err)
	}
	if err := h.autos.Save(ctx, record); err != nil {
		t.Fatal(err)
	}
	if _, err := newPhaseLauncher(h.deps).launch(ctx, rolled, ""); err != nil {
		t.Fatal(err)
	}
	audit, _ := applyAudit(h)
	if audit.Details["level"] != "act-within-policy" {
		t.Errorf("audit %+v", audit)
	}
	_ = errors.New
}
