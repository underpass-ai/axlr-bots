package application

import (
	"context"
	"time"

	"github.com/underpass-ai/underpass-bots/domain"
)

// settler records how a run ended: it applies the runtime's report to the
// execution and the task, persists both, publishes the outcome event and
// audits the action. RunTask and ApproveTask end their runs through it.
type settler struct {
	deps Dependencies
	rec  recorder
}

func newSettler(deps Dependencies) settler {
	return settler{deps: deps, rec: recorder{deps: deps}}
}

// settle ends the execution and the task as the report says, or as Failed
// when the executor itself returned an error. started is when the
// execution began, so a clock that runs backwards cannot end it earlier.
func (s settler) settle(ctx context.Context, action string, bot domain.Bot, task *domain.Task, execution *domain.Execution, report ExecutionReport, runErr error, started time.Time) error {
	if runErr != nil {
		report = ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "runtime: " + runErr.Error()}
	}
	ended := s.deps.Clock.Now()
	if ended.Before(started) {
		ended = started
	}
	eventType, err := apply(task, execution, report, ended)
	if err != nil {
		return err
	}
	if err := s.deps.Executions.Save(ctx, *execution); err != nil {
		return err
	}
	if err := s.deps.Tasks.Save(ctx, *task); err != nil {
		return err
	}
	payload := map[string]any{"executionId": execution.ID().String(), "outcome": execution.Outcome().String()}
	if execution.FailureReason() != "" {
		payload["reason"] = execution.FailureReason()
	}
	if err := s.rec.event(ctx, eventType, bot, task.ID(), task.CorrelationID(), payload); err != nil {
		return err
	}
	auditOutcome := domain.AuditOK
	if execution.Outcome() == domain.ExecutionFailed {
		auditOutcome = domain.AuditFailed
	}
	return s.rec.audit(ctx, action, task.ID().String(), auditOutcome, map[string]string{"execution": execution.ID().String(), "outcome": execution.Outcome().String()})
}

// apply moves the execution and the task to where the report says and
// names the event that tells it.
func apply(task *domain.Task, execution *domain.Execution, report ExecutionReport, ended time.Time) (domain.EventType, error) {
	switch report.Outcome {
	case domain.ExecutionCompleted:
		if err := execution.Complete(report.Result, report.Usage, report.TracePath, ended); err != nil {
			return "", err
		}
		return domain.EventTaskCompleted, task.Complete(ended)
	case domain.ExecutionDecisionRequired:
		if err := execution.RequireDecision(report.Result, report.Usage, report.TracePath, ended); err != nil {
			return "", err
		}
		return domain.EventTaskWaitingApproval, task.AwaitApproval(ended)
	default:
		reason := report.FailureReason
		if reason == "" {
			reason = "runtime reported " + string(report.Outcome) + " without a reason"
		}
		if err := execution.Fail(reason, report.Result, report.Usage, report.TracePath, ended); err != nil {
			return "", err
		}
		return domain.EventTaskFailed, task.Fail(reason, ended)
	}
}
