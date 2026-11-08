package application

// Checks that hold: these PASS on fff6ed4 and mark a clean area.

import (
	"context"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

func pinnedUnattended(t *testing.T, h *harness, incident domain.Incident) bool {
	t.Helper()
	task, _ := h.tasks.Get(context.Background(), incident.CurrentTaskID())
	pinned, ok := task.Pinned()
	return ok && pinned.Unattended()
}

func TestS01_ALoweredRunbookGetsTheCardAtTheNextLaunch(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	a := ladderProposed(t, h, otherIncident(triage, "group-a", "fp-a"))
	runCurrent(t, h, a.ID(), completes("patched", 0.01))
	runCurrent(t, h, a.ID(), completes("VERDICT: red", 0.01))
	b := ladderProposed(t, h, otherIncident(triage, "group-b", "fp-b"))
	if pinnedUnattended(t, h, b) {
		t.Error("a lowered runbook was granted")
	}
}

func TestS02_TwoNamespacesAtLaunchKeepTheCard(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	command := shopAlert(triage, "sev2", ladderNamespace)
	opened, err := NewOpenIncident(h.deps).Execute(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	second := command
	second.Namespace, second.Fingerprints = "payments", []string{"fp-b"}
	if _, err := NewOpenIncident(h.deps).Execute(ctx, second); err != nil {
		t.Fatal(err)
	}
	if incident := throughFix(t, h, opened.Incident.ID()); pinnedUnattended(t, h, incident) {
		t.Error("two namespaces were granted")
	}
}

// A CloudWatch incident has no namespace at all: it can never be granted,
// and the "<AlarmName>: <reason>" parse of the ADR never reaches a unit.
func TestS03_CloudWatchHasNoUnit(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	command := shopAlert(triage, "sev2", "")
	command.Source, command.Title, command.GroupKey = "cloudwatch", ladderRunbook+": threshold crossed", "cw-group"
	if incident := ladderProposed(t, h, command); pinnedUnattended(t, h, incident) {
		t.Error("an incident of no namespace was granted")
	}
}

func TestS04_AVerifyRedLowersAnApplyThatActedAlone(t *testing.T) {
	h := newHarness()
	triage := topped(t, h, domain.ApprovalsSnapshot{})
	incident := ladderProposed(t, h, shopAlert(triage, "sev2", ladderNamespace))
	runCurrent(t, h, incident.ID(), completes("patched", 0.01))
	runCurrent(t, h, incident.ID(), completes("VERDICT: red", 0.01))
	if step := autonomyOf(t, h, incident).Step(); step != domain.AutonomyPropose {
		t.Errorf("step %s", step)
	}
}

func TestS05_ManualTaskOnALadderBotCarriesNoGrant(t *testing.T) {
	h := newHarness()
	ladderTeam(t, h, nil, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	fix, _ := h.bots.Get(ctx, "fix")
	policy := fix.PolicyFor(domain.ManualTrigger())
	if policy.Unattended() {
		t.Error("a manual task is unattended")
	}
}
