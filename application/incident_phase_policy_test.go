package application

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

// A phase task keeps the policy it was submitted with: a trigger widened
// meanwhile does not widen its run, and a trigger removed refuses its
// approval (review of #133, R2).
func TestAPhaseTaskKeepsThePolicyItWasSubmittedWith(t *testing.T) {
	h := newHarness()
	triage := brigade(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := proposed(t, h, triage)
	apply, _ := h.tasks.Get(ctx, incident.CurrentTaskID())
	pinned, ok := apply.Pinned()
	if !ok || !slices.Equal(pinned.Tools().Names(), []string{"local_read", "local_write"}) || apply.Snapshot().Policy == nil {
		t.Fatalf("pinned = %+v %v", apply.Snapshot().Policy, ok)
	}
	wider := []domain.TriggerSnapshot{
		{Type: "incident", Name: "fix", Phase: "fix", Level: "propose", Tools: []string{"local_read"}},
		{Type: "incident", Name: "apply", Phase: "apply", Tools: []string{"local_read", "local_search", "local_write"}},
	}
	if _, err := NewUpdateBot(h.deps).Execute(ctx, "fix", BotPatch{Triggers: &wider}); err != nil {
		t.Fatal(err)
	}
	waiting := waitingApply(t, h, incident)
	if run := h.runtime.requests[len(h.runtime.requests)-1]; slices.Contains(run.AllowedTools, "local_search") {
		t.Errorf("a widened trigger widened the run: %v", run.AllowedTools)
	}
	renamed := []domain.TriggerSnapshot{wider[0], {Type: "incident", Name: "apply2", Phase: "apply", Tools: []string{"local_write"}}}
	if _, err := NewUpdateBot(h.deps).Execute(ctx, "fix", BotPatch{Triggers: &renamed}); err != nil {
		t.Fatal(err)
	}
	decisions := len(h.decider.requests)
	var state domain.StateError
	if _, err := NewApproveTask(h.deps).Execute(ctx, waiting.ID(), DecisionApprove, ""); !errors.As(err, &state) || len(h.decider.requests) != decisions {
		t.Fatalf("approve under a removed trigger: %v", err)
	}
	h.decider.report = completes("not applied", 0)
	if _, err := NewApproveTask(h.deps).Execute(ctx, waiting.ID(), DecisionDeny, ""); err != nil {
		t.Fatalf("deny under a removed trigger: %v", err)
	}
	if d := h.decider.requests[len(h.decider.requests)-1]; slices.Contains(d.AllowedTools, "local_search") || d.Level != domain.AutonomyActWithApproval {
		t.Errorf("the denial ran at %s with %v", d.Level, d.AllowedTools)
	}
}

// On a node, a narrowed phase gets exactly its tools: no delegate, emit
// or subscribe tool it does not list (review of #133, R8).
func TestANarrowedPhaseGetsExactlyItsToolsOnANode(t *testing.T) {
	h := newHarness()
	triage := brigade(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := proposed(t, h, triage)
	node := onlineNode(t, h, "web-1")
	assignment, found, err := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	if err != nil || !found || assignment.Run == nil || assignment.Run.TaskID != incident.CurrentTaskID() {
		t.Fatalf("assignment: %v %+v", err, assignment)
	}
	if !slices.Equal(assignment.Run.AllowedTools, []string{"local_read", "local_write"}) || assignment.Run.Events != nil || assignment.Run.Delegation != nil {
		t.Errorf("the node's run of the apply: tools %v, events %v, delegation %v", assignment.Run.AllowedTools, assignment.Run.Events, assignment.Run.Delegation)
	}
}

// A phase task left with no tool its bot still allows fails with the
// reason, in process, on a node and at its decision, rather than run with
// an empty allow-list, which the runtime reads as "the level alone
// decides" (re-check of #133, N1).
func TestAPhaseTaskLeftWithNoToolFails(t *testing.T) {
	retool := func(t *testing.T, h *harness) {
		t.Helper()
		tools := []string{"kube_get", "kube_apply"}
		triggers := []domain.TriggerSnapshot{
			{Type: "incident", Name: "fix", Phase: "fix", Level: "propose", Tools: []string{"kube_get"}},
			{Type: "incident", Name: "apply", Phase: "apply", Tools: []string{"kube_apply"}},
		}
		if _, err := NewUpdateBot(h.deps).Execute(context.Background(), "fix", BotPatch{AllowedTools: &tools, Triggers: &triggers}); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	failedWith := func(t *testing.T, h *harness, id domain.TaskID) {
		t.Helper()
		task, _ := h.tasks.Get(ctx, id)
		if task.Status() != domain.TaskFailed || !strings.Contains(task.FailureReason(), "policy refused") || !strings.Contains(task.FailureReason(), "no tool") {
			t.Fatalf("task %s: %q", task.Status(), task.FailureReason())
		}
	}

	h := newHarness()
	incident := proposed(t, h, brigade(t, h, domain.ApprovalsSnapshot{}))
	retool(t, h)
	runs := len(h.runtime.requests)
	if _, err := NewRunTask(h.deps).Execute(ctx, incident.CurrentTaskID()); err != nil {
		t.Fatal(err)
	}
	if len(h.runtime.requests) != runs {
		t.Error("the runtime was given the task")
	}
	failedWith(t, h, incident.CurrentTaskID())
	if back, _ := h.incs.Get(ctx, incident.ID()); back.Status() != domain.IncidentOpen || back.SendBacks() != 1 {
		t.Errorf("the incident after a refused apply: %s %d", back.Status(), back.SendBacks())
	}

	h = newHarness()
	incident = proposed(t, h, brigade(t, h, domain.ApprovalsSnapshot{}))
	retool(t, h)
	node := onlineNode(t, h, "web-1")
	if assignment, found, err := NewNextAssignment(h.deps).Execute(ctx, node.ID()); err != nil || (found && assignment.Run != nil && assignment.Run.TaskID == incident.CurrentTaskID()) {
		t.Fatalf("the node was given the task: %v %+v", err, assignment)
	}
	failedWith(t, h, incident.CurrentTaskID())

	h = newHarness()
	incident = proposed(t, h, brigade(t, h, domain.ApprovalsSnapshot{}))
	waiting := waitingApply(t, h, incident)
	retool(t, h)
	decisions := len(h.decider.requests)
	if _, err := NewApproveTask(h.deps).Execute(ctx, waiting.ID(), DecisionDeny, ""); err != nil {
		t.Fatal(err)
	}
	if len(h.decider.requests) != decisions {
		t.Error("the decision ran with no tool")
	}
	failedWith(t, h, waiting.ID())
}

// Below act-with-approval a phase task never keeps a host write tool:
// lowering the bot under a phase that lists one is refused, and a task
// pinned with one before the bot was lowered runs without it (re-check of
// #133, N2).
func TestALowerLevelDropsTheHostWriteTools(t *testing.T) {
	h := newHarness()
	incident := proposed(t, h, brigade(t, h, domain.ApprovalsSnapshot{}))
	ctx := context.Background()
	lower := "propose"
	if _, err := NewUpdateBot(h.deps).Execute(ctx, "fix", BotPatch{Level: &lower}); !isValidation(err) {
		t.Fatalf("lowering the bot under an apply phase listing local_write: %v", err)
	}
	triggers := []domain.TriggerSnapshot{
		{Type: "incident", Name: "fix", Phase: "fix", Level: "propose", Tools: []string{"local_read"}},
		{Type: "incident", Name: "apply", Phase: "apply", Tools: []string{"local_read"}},
	}
	if _, err := NewUpdateBot(h.deps).Execute(ctx, "fix", BotPatch{Level: &lower, Triggers: &triggers}); err != nil {
		t.Fatal(err)
	}
	h.runtime.report = completes("APPLIED", 0)
	if _, err := NewRunTask(h.deps).Execute(ctx, incident.CurrentTaskID()); err != nil {
		t.Fatal(err)
	}
	run := h.runtime.requests[len(h.runtime.requests)-1]
	if run.Level != domain.AutonomyPropose || slices.Contains(run.AllowedTools, "local_write") || !slices.Contains(run.AllowedTools, "local_read") {
		t.Errorf("the pinned apply at a lowered bot: %s %v", run.Level, run.AllowedTools)
	}
}
