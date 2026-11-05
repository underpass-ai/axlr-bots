package application

import (
	"context"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

// A run a cancel stopped on its node reports after: the report is not
// accepted, but what the run consumed is kept on the cancelled execution
// and added to the task's incident. Before, the run counted as $0.
func TestALateReportKeepsTheCostOfACancelledNodeRun(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := triageBot(t, h, "triage", "")
	node := onlineNode(t, h, "web-1")
	opened, err := NewOpenIncident(h.deps).Execute(ctx, alert(bot, "fp-a"))
	if err != nil {
		t.Fatal(err)
	}
	assignment := nextFor(t, h, node.ID())
	cancelled, err := NewCancelTask(h.deps).Execute(ctx, opened.Task.ID())
	if err != nil || cancelled.Execution == nil || !cancelled.Execution.Usage().CostUnknown {
		t.Fatalf("cancel = %+v %v", cancelled.Execution, err)
	}
	unknown := ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "interrupted: cancelled", Usage: domain.UnknownUsage(2)}
	if late, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), assignment.ExecutionID, unknown); err != nil || late.Accepted || !late.Execution.Usage().CostUnknown {
		t.Fatalf("a late report that does not know either = %+v %v", late, err)
	}
	report := ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "interrupted: cancelled", Usage: domain.Usage{Requests: 2, PromptTokens: 900, CostUSD: 0.01}, Receipt: []byte(`{"schema":"axlr.execution-receipt/v1"}`)}
	late, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), assignment.ExecutionID, report)
	if err != nil || late.Accepted || late.Task.Status() != domain.TaskCancelled {
		t.Fatalf("late report = %+v %v", late, err)
	}
	stored, _ := h.execs.Get(ctx, assignment.ExecutionID)
	if stored.Outcome() != domain.ExecutionCancelled || stored.Usage().CostUSD != 0.01 || stored.Usage().Requests != 2 || stored.Usage().CostUnknown || len(stored.Receipt()) == 0 {
		t.Errorf("execution = %s %+v", stored.Outcome(), stored.Usage())
	}
	if incident, _ := h.incs.Get(ctx, opened.Incident.ID()); incident.CostUSD() != 0.01 {
		t.Errorf("incident cost = %v", incident.CostUSD())
	}
	// A second late report changes nothing: the cost is known now.
	report.Usage.CostUSD = 0.5
	if _, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), assignment.ExecutionID, report); err != nil {
		t.Fatal(err)
	}
	if again, _ := h.execs.Get(ctx, assignment.ExecutionID); again.Usage().CostUSD != 0.01 {
		t.Errorf("the second report changed the cost: %+v", again.Usage())
	}
	if incident, _ := h.incs.Get(ctx, opened.Incident.ID()); incident.CostUSD() != 0.01 {
		t.Errorf("incident cost counted twice = %v", incident.CostUSD())
	}
}

// A cancelled run of a task with no incident keeps its late cost too; a
// store that fails the save is the report's error.
func TestALateReportOfATaskWithoutAnIncident(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	node := onlineNode(t, h, "web-1")
	assignment := runOnNode(t, h, bot, node.ID(), "k")
	if _, err := NewCancelTask(h.deps).Execute(ctx, assignment.Run.TaskID); err != nil {
		t.Fatal(err)
	}
	report := ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "interrupted", Usage: domain.Usage{Requests: 1, CostUSD: 0.002}}
	h.execs.fail = errBoom
	if _, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), assignment.ExecutionID, report); err == nil {
		t.Error("a failing save was not the report's error")
	}
	h.execs.fail = nil
	if _, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), assignment.ExecutionID, report); err != nil {
		t.Fatal(err)
	}
	if stored, _ := h.execs.Get(ctx, assignment.ExecutionID); stored.Usage().CostUSD != 0.002 {
		t.Errorf("usage = %+v", stored.Usage())
	}
}

// A decision still queued for its node never ran: its cancel knows it
// cost nothing.
func TestACancelledQueuedDecisionCostNothing(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	task, decided := queuedDecision(t, h)
	if _, err := NewCancelTask(h.deps).Execute(ctx, task.ID()); err != nil {
		t.Fatal(err)
	}
	if stored, _ := h.execs.Get(ctx, decided.Execution.ID()); stored.Outcome() != domain.ExecutionCancelled || stored.Usage().CostUnknown {
		t.Errorf("execution = %s %+v", stored.Outcome(), stored.Usage())
	}
}
