package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ReportExecution takes a node's report of how an execution ended and
// settles the task exactly as an in-process run does. Reporting is
// idempotent: a report for an execution that has already ended is
// accepted and changes nothing, so a node may retry without fear.
type ReportExecution struct {
	deps Dependencies
	rec  recorder
	set  settler
}

// NewReportExecution wires the use case.
func NewReportExecution(deps Dependencies) ReportExecution {
	return ReportExecution{deps: deps, rec: recorder{deps: deps}, set: newSettler(deps)}
}

// Execute settles the execution and returns it with its task; Accepted
// is false when the execution had already ended.
func (uc ReportExecution) Execute(ctx context.Context, node domain.NodeID, id domain.ExecutionID, report ExecutionReport) (ReportResult, error) {
	execution, err := uc.deps.Executions.Get(ctx, id)
	if err != nil {
		return ReportResult{}, err
	}
	if execution.NodeID() != node {
		return ReportResult{}, domain.StateError{Subject: "execution " + id.String(), Current: "assigned to " + orNobody(execution.NodeID()), Attempted: "report for node " + node.String()}
	}
	task, err := uc.deps.Tasks.Get(ctx, execution.TaskID())
	if err != nil {
		return ReportResult{}, err
	}
	if execution.Outcome() != domain.ExecutionRunning {
		return ReportResult{Task: task, Execution: execution, Accepted: false}, nil
	}
	bot, err := uc.deps.Bots.Get(ctx, task.BotID())
	if err != nil {
		return ReportResult{}, err
	}
	action := "task.run"
	if execution.Kind() == domain.ExecutionDecide {
		action = "task.decide"
	}
	if err := uc.set.settle(ctx, action, bot, &task, &execution, report, nil, execution.StartedAt()); err != nil {
		return ReportResult{}, err
	}
	payload := map[string]any{"executionId": execution.ID().String(), "nodeId": node.String(), "outcome": execution.Outcome().String()}
	if err := uc.rec.event(ctx, domain.EventExecutionReported, bot, task.ID(), task.CorrelationID(), payload); err != nil {
		return ReportResult{}, err
	}
	return ReportResult{Task: task, Execution: execution, Accepted: true}, nil
}
