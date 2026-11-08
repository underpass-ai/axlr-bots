package application

import (
	"context"
	"errors"
	"slices"
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
