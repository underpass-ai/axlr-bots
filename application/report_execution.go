package application

import (
	"context"
	"errors"

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
		// A late report: the run's usage is still kept, when the end
		// (a cancel) could not know it.
		if err := newLateAccount(uc.deps).keep(ctx, task, &execution, report); err != nil {
			return ReportResult{}, err
		}
		return uc.withMemory(ctx, ReportResult{Task: task, Execution: execution, Accepted: false}, nil)
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
		// A reconcile ended the execution between the read and the save:
		// its end stands and the report is late.
		var conflict domain.ConflictError
		if !errors.As(err, &conflict) {
			return ReportResult{}, err
		}
		return uc.late(ctx, id)
	}
	payload := map[string]any{"executionId": execution.ID().String(), "nodeId": node.String(), "outcome": execution.Outcome().String()}
	if err := uc.rec.executionEvent(ctx, domain.EventExecutionReported, bot, task.ID(), execution.ID(), task.CorrelationID(), payload); err != nil {
		return ReportResult{}, err
	}
	return uc.withMemory(ctx, ReportResult{Task: task, Execution: execution, Accepted: true}, &bot)
}

// withMemory adds the answering team's record to the answer of a report
// on a question's task that ended. A record that cannot be made leaves the
// answer without it; the first report that carries it is audited.
func (uc ReportExecution) withMemory(ctx context.Context, result ReportResult, bot *domain.Bot) (ReportResult, error) {
	if result.Task.Trigger().Type != domain.TriggerQuestion || !result.Task.Status().IsTerminal() {
		return result, nil
	}
	var responder domain.Bot
	if bot != nil {
		responder = *bot
	} else {
		found, err := uc.deps.Bots.Get(ctx, result.Task.BotID())
		if domain.IsNotFound(err) {
			return result, nil
		}
		if err != nil {
			return ReportResult{}, err
		}
		responder = found
	}
	record, err := channelExchange{responder: responder, question: result.Task, execution: result.Execution}.record(domain.ChannelAnswered)
	if !result.Accepted {
		if err == nil {
			result.ChannelMemory = &record
		}
		return result, nil
	}
	if err != nil {
		return result, uc.rec.audit(ctx, "question.memory", result.Task.ID().String(), domain.AuditDenied, map[string]string{"side": domain.ChannelAnswered.String(), "reason": err.Error()})
	}
	result.ChannelMemory = &record
	details := map[string]string{"side": record.Side.String(), "team": record.Team.String(), "about": record.About(), "mode": "report", "node": result.Execution.NodeID().String()}
	return result, uc.rec.audit(ctx, "question.memory", result.Task.ID().String(), domain.AuditOK, details)
}

// late answers a report whose execution ended meanwhile: not accepted,
// with the task and the execution as they now stand.
func (uc ReportExecution) late(ctx context.Context, id domain.ExecutionID) (ReportResult, error) {
	execution, err := uc.deps.Executions.Get(ctx, id)
	if err != nil {
		return ReportResult{}, err
	}
	task, err := uc.deps.Tasks.Get(ctx, execution.TaskID())
	if err != nil {
		return ReportResult{}, err
	}
	return uc.withMemory(ctx, ReportResult{Task: task, Execution: execution, Accepted: false}, nil)
}
