package application

// Round 2 of the review of #143 (7ef2380). FAIL = confirmed defect or gap;
// a test named Sound_ passes and marks what holds.

import (
	"context"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

type leaseCrash struct {
	ExecutionRepository
	armed *bool
}

func (l leaseCrash) Save(ctx context.Context, e domain.Execution) error {
	if *l.armed && !e.LeaseUntil().IsZero() {
		*l.armed = false
		return errBoom
	}
	return l.ExecutionRepository.Save(ctx, e)
}

// Item 1: a crash between startPending's saves and the lease re-delivers
// the run through queued(), which has no recheck.
func TestN01_ReDeliveredAssignmentSkipsTheRecheck(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	node := onlineNode(t, h, "web-1")
	armed := true
	crashing := h.deps
	crashing.Executions = leaseCrash{ExecutionRepository: h.deps.Executions, armed: &armed}
	if _, _, err := NewNextAssignment(crashing).Execute(ctx, node.ID()); err == nil {
		t.Fatal("setup: the lease did not fail")
	}
	if _, err := NewLowerAutonomy(h.deps).Execute(ctx, autonomyOf(t, h, incident).ID(), "tirso", "stop"); err != nil {
		t.Fatal(err)
	}
	assignment, found, err := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	if err != nil || !found || assignment.Run == nil {
		t.Fatalf("no re-delivery: %v %v", found, err)
	}
	if assignment.Run.Unattended {
		t.Errorf("re-delivered with --approve all after a person lowered the runbook to %s", autonomyOf(t, h, incident).Step())
	}
}

// Item 1/4: a node that took the assignment is not told of a lowering.
func TestN02_LoweringDoesNotStopARunAlreadyTaken(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	node := onlineNode(t, h, "web-1")
	first, found, err := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	if err != nil || !found || !first.Run.Unattended {
		t.Fatalf("setup: %v %v", found, err)
	}
	if _, err := NewLowerAutonomy(h.deps).Execute(ctx, autonomyOf(t, h, incident).ID(), "tirso", "stop"); err != nil {
		t.Fatal(err)
	}
	task, _ := h.tasks.Get(ctx, incident.CurrentTaskID())
	if task.Status() == domain.TaskRunning {
		t.Errorf("runbook lowered by a person, the unattended run stays %s and nobody tells its node", task.Status())
	}
}

// Item 2: failure with a session and no reported call ends the entry.
func TestN04_FailedRunThatReportsNoCallsHidesAWrite(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	runCurrent(t, h, incident.ID(), ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "runtime reported failed", Result: domain.Result{SessionID: "s"}, Usage: domain.Usage{ToolCalls: 1}})
	record := autonomyOf(t, h, incident)
	if record.Step() == domain.AutonomyActWithinPolicy && len(record.Outstanding()) == 0 {
		t.Errorf("Usage says one tool call ran, ToolCalls is empty: treated as wrote nothing, entry gone, still at the top")
	}
}

// Item 3: uniform then a differing delivery before the run.
func TestN05_Sound_ADifferingAlertBeforeTheRunTakesBackTheGrant(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	command := shopAlert(triage, "sev2", ladderNamespace)
	incident := ladderProposed(t, h, command)
	second := command
	second.alerts, second.Fingerprints = domain.AlertGroupMixed, []string{"fp-b"}
	if _, err := NewOpenIncident(h.deps).Execute(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	runCurrent(t, h, incident.ID(), completes("patched", 0.01))
	if lastUnattended(h) {
		t.Error("granted after the group went mixed")
	}
}

// Item 3: a hand-opened or pre-upgrade incident, Unknown, becomes Uniform
// on one later delivery: the code comment says it never acts alone.
func TestN06_UnknownIncidentBecomesUniformOnALaterDelivery(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	command := shopAlert(triage, "sev2", ladderNamespace)
	command.alerts = domain.AlertGroupUnknown
	opened, err := NewOpenIncident(h.deps).Execute(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	delivery := command
	delivery.alerts, delivery.Fingerprints = domain.AlertGroupUniform, []string{"fp-b"}
	if _, err := NewOpenIncident(h.deps).Execute(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	incident := throughFix(t, h, opened.Incident.ID())
	if pinnedUnattended(t, h, incident) {
		t.Errorf("an incident opened by hand (or stored by an older version) was granted once one delivery said uniform")
	}
}

func payloadCommand(t *testing.T, triage domain.Bot, body string) (OpenIncidentCommand, domain.AlertmanagerPayload) {
	t.Helper()
	payload, err := domain.ParseAlertmanagerPayload([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return OpenIncidentCommand{alerts: alertGroupOf(payload), Tenant: "default", BotID: triage.ID().String(), GroupKey: payload.GroupKey, Source: "alertmanager", Title: payload.Title(), Severity: payload.Severity().String(), Namespace: "shop", Fingerprints: payload.FiringFingerprints()}, payload
}

// F3 residual: the label decides the severity.
func TestN08_SeverityLabelsOutsideTheMapReadAsSev3AndAreGranted(t *testing.T) {
	for _, label := range []string{"sev1", "P1", "high", "disaster"} {
		t.Run(label, func(t *testing.T) {
			h := newHarness()
			triage := topped(t, h, domain.ApprovalsSnapshot{})
			command, payload := payloadCommand(t, triage, `{"version":"4","groupKey":"k","status":"firing","groupLabels":{"alertname":"KubePodCrashLooping","namespace":"shop"},"commonLabels":{"severity":"`+label+`"},"alerts":[{"status":"firing","fingerprint":"f","labels":{"alertname":"KubePodCrashLooping","namespace":"shop","severity":"`+label+`"}}]}`)
			incident := ladderProposed(t, h, command)
			if pinnedUnattended(t, h, incident) {
				t.Errorf("severity label %q read %s and the apply was granted", label, payload.Severity())
			}
		})
	}
}

// Item 7 / F11: a bridged call that never reached the tool counts as a
// write when its status is failed.
func TestN10_FailedBridgedCallsCountAsWrites(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	report := completes("patched", 0.01)
	report.Result.ToolCalls = []domain.ToolCall{{Name: "axlr_call_tool", CallID: "c1", Status: "failed"}, {Name: "axlr_call_tool", CallID: "c2", Status: "completed"}}
	runCurrent(t, h, incident.ID(), report)
	if record := autonomyOf(t, h, incident); record.Seen(incident.ID(), incident.Cycle(), domain.SignalApplyOverrun) {
		t.Errorf("one call failed to resolve, one write ran: apply-overrun, step %s, earned %+v", record.Step(), record.Earned())
	}
}

// Item 4.
func TestN11_Sound_LowerCannotRaiseAndNeedsAnApprover(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	id := autonomyOf(t, h, incident).ID()
	for _, actor := range []string{"", "someone", "bot:x", "policy:y", "fix"} {
		if _, err := NewLowerAutonomy(h.deps).Execute(ctx, id, actor, "x"); err == nil {
			t.Errorf("%q lowered", actor)
		}
	}
	for want := range 4 {
		view, err := NewLowerAutonomy(h.deps).Execute(ctx, id, "tirso", "x")
		if err != nil {
			t.Fatal(err)
		}
		if view.Autonomy.Earned() != (domain.AutonomyCounters{}) || view.Autonomy.Step() == domain.AutonomyActWithinPolicy {
			t.Errorf("after %d: %s %+v", want, view.Autonomy.Step(), view.Autonomy.Earned())
		}
	}
}

// Item 4: reason is stored as given.
func TestN12_LowerReasonIsUnbounded(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	reason := strings.Repeat("x", 900*1024) + "\x1b[2J"
	view, err := NewLowerAutonomy(h.deps).Execute(context.Background(), autonomyOf(t, h, incident).ID(), "tirso", reason)
	if err == nil && len(view.Autonomy.History()) > 0 && len(view.Autonomy.History()[len(view.Autonomy.History())-1].Reason) > 1024 {
		t.Errorf("a 900 KiB reason with control characters is kept in the record and the panel history")
	}
}

// Item 7: the alias in the title is closed even when the group is uniform.
func TestN13_Sound_AliasWithAUniformGroupKeepsTheCard(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	command, _ := payloadCommand(t, triage, `{"version":"4","groupKey":"k","status":"firing","groupLabels":{"alertname":"KubePodCrashLooping in disguise","namespace":"shop"},"alerts":[{"status":"firing","fingerprint":"f","labels":{}}]}`)
	if incident := ladderProposed(t, h, command); pinnedUnattended(t, h, incident) {
		t.Error("alias granted")
	}
}

func TestN03_NoLivenessTrap(t *testing.T) {
	setup := func(t *testing.T) (*harness, domain.Incident) {
		h := newHarness()
		triage := topped(t, h, domain.ApprovalsSnapshot{})
		return h, ladderProposed(t, h, otherIncident(triage, "group-a", "fp-a"))
	}
	ctx := context.Background()
	t.Run("granted task cancelled before it ran", func(t *testing.T) {
		h, a := setup(t)
		if _, err := NewCancelTask(h.deps).Execute(ctx, a.CurrentTaskID()); err != nil {
			t.Fatal(err)
		}
		if got := autonomyOf(t, h, a).Outstanding(); len(got) != 0 {
			t.Errorf("a task that never ran holds the unit: %+v", got)
		}
	})
	t.Run("clear ends the entry and costs nothing", func(t *testing.T) {
		h, a := setup(t)
		runCurrent(t, h, a.ID(), completes("patched", 0.01))
		runCurrent(t, h, a.ID(), completes("no verdict", 0.01))
		before := autonomyOf(t, h, a)
		if len(before.Outstanding()) != 1 {
			t.Fatalf("setup: %+v", before.Outstanding())
		}
		for _, actor := range []string{"", "someone", "policy:y", "fix"} {
			if _, err := NewClearAutonomy(h.deps).Execute(ctx, before.ID(), actor, "x"); err == nil {
				t.Errorf("%q cleared", actor)
			}
		}
		view, err := NewClearAutonomy(h.deps).Execute(ctx, before.ID(), "tirso", "looked")
		if err != nil {
			t.Fatal(err)
		}
		if view.Autonomy.Step() != before.Step() || view.Autonomy.Earned() != before.Earned() || len(view.Autonomy.Outstanding()) != 0 {
			t.Errorf("after clear: %+v", view.Autonomy.Snapshot())
		}
		if view, err = NewClearAutonomy(h.deps).Execute(ctx, before.ID(), "tirso", "again"); err != nil || len(view.Autonomy.History()) != len(autonomyOf(t, h, a).History()) {
			t.Errorf("a second clear: %v", err)
		}
	})
	t.Run("a tool edit resets the entry", func(t *testing.T) {
		h, a := setup(t)
		editFix(t, h, BotPatch{Triggers: ptr(fixTriggers(&domain.LadderSnapshot{Runbooks: []domain.LadderRunbookSnapshot{{Name: ladderRunbook, Tool: "scale", Namespaces: []string{ladderNamespace}}}}, true))})
		b := ladderProposed(t, h, otherIncident(triageOf(t, h), "group-b", "fp-b"))
		if got := autonomyOf(t, h, b).Outstanding(); len(got) != 0 {
			t.Errorf("outstanding after a tool edit: %+v", got)
		}
		_ = a
	})
	t.Run("lower and clear refuse what cannot be read", func(t *testing.T) {
		h, a := setup(t)
		id := autonomyOf(t, h, a).ID()
		h.autos.fail = errBoom
		if _, err := NewLowerAutonomy(h.deps).Execute(ctx, id, "tirso", ""); err == nil {
			t.Error("lower of an unreadable record")
		}
		if _, err := NewClearAutonomy(h.deps).Execute(ctx, id, "tirso", ""); err == nil {
			t.Error("clear of an unreadable record")
		}
	})
}

func TestN05_ABotMayNotTakeAnApproversName(t *testing.T) {
	h := newHarness()
	topped(t, h, domain.ApprovalsSnapshot{})
	d := sampleDefinition()
	d.ID, d.Name, d.Tenant = "tirso", "tirso", "default"
	if _, err := NewRegisterBot(h.deps).Execute(context.Background(), d); err == nil || !strings.Contains(err.Error(), "approver") {
		t.Fatalf("registered: %v", err)
	}
}

func TestN02_AnUnleasedRunIsNotCancelledButALeasedOneIs(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	node := onlineNode(t, h, "web-1")
	if _, _, err := NewNextAssignment(h.deps).Execute(ctx, node.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLowerAutonomy(h.deps).Execute(ctx, autonomyOf(t, h, incident).ID(), "tirso", "stop"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range h.log.audits {
		found = found || (entry.Action == "autonomy.changed" && entry.Details["cancelled"] == "1")
	}
	if !found {
		t.Errorf("the cancel was not audited on the lowering: %+v", h.log.audits)
	}
}

func TestSeverityLabelsTheMappingNames(t *testing.T) {
	for _, c := range []struct {
		label string
		grant bool
	}{{"warning", true}, {"error", true}, {"", false}, {"critical", false}} {
		h := newHarness()
		triage := topped(t, h, domain.ApprovalsSnapshot{})
		labels := `"alertname":"KubePodCrashLooping","namespace":"shop"`
		if c.label != "" {
			labels += `,"severity":"` + c.label + `"`
		}
		command, _ := payloadCommand(t, triage, `{"version":"4","groupKey":"k","status":"firing","groupLabels":{"alertname":"KubePodCrashLooping","namespace":"shop"},"alerts":[{"status":"firing","fingerprint":"f","labels":{`+labels+`}}]}`)
		if got := pinnedUnattended(t, h, ladderProposed(t, h, command)); got != c.grant {
			t.Errorf("%q: granted %v", c.label, got)
		}
	}
}

func triageOf(t *testing.T, h *harness) domain.Bot {
	t.Helper()
	bot, err := h.bots.Get(context.Background(), "triage")
	if err != nil {
		t.Fatal(err)
	}
	return bot
}
