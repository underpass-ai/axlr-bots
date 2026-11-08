package application

import (
	"context"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

// The run that stopped on the apply's call adds its cost, and a task's
// end seen twice adds it once (review of #133, R3 and R7).
func TestAnIncidentCountsEachExecutionOnce(t *testing.T) {
	h := newHarness()
	triage := brigade(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := proposed(t, h, triage)
	before := incident.CostUSD()
	h.runtime.report = ExecutionReport{Outcome: domain.ExecutionDecisionRequired, Usage: domain.Usage{CostUSD: 0.5}, Result: domain.Result{SessionID: "s", PendingDecision: &domain.PendingDecision{Tool: "local_write", CallID: "c", SessionID: "s"}}}
	waiting, err := NewRunTask(h.deps).Execute(ctx, incident.CurrentTaskID())
	if err != nil {
		t.Fatal(err)
	}
	if after, _ := h.incs.Get(ctx, incident.ID()); after.CostUSD()-before < 0.499 {
		t.Fatalf("the waiting run's cost: %v", after.CostUSD()-before)
	}
	h.decider.report = completes("APPLIED", 0.02)
	decided, err := NewApproveTask(h.deps).Execute(ctx, waiting.Task.ID(), DecisionApprove, "")
	if err != nil {
		t.Fatal(err)
	}
	after, _ := h.incs.Get(ctx, incident.ID())
	if got := after.CostUSD() - before; got < 0.519 || got > 0.521 {
		t.Fatalf("waiting 0.5 + decision 0.02 = %v", got)
	}
	bot, _ := h.bots.Get(ctx, "fix")
	task, _ := h.tasks.Get(ctx, decided.Task.ID())
	if err := newIncidentAdvancer(h.deps).taskEnded(ctx, bot, task, decided.Execution); err != nil {
		t.Fatal(err)
	}
	again, _ := h.incs.Get(ctx, incident.ID())
	if again.CostUSD() != after.CostUSD() {
		t.Errorf("an end seen twice added %v", again.CostUSD()-after.CostUSD())
	}
}
