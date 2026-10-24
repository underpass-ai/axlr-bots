package application

import (
	"context"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
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
// A completed run's answer is kept as the task's report artifact.
func (s settler) settle(ctx context.Context, action string, bot domain.Bot, task *domain.Task, execution *domain.Execution, report ExecutionReport, runErr error, started time.Time) error {
	if dropped, err := s.dropIfEnded(ctx, action, task, execution); dropped || err != nil {
		return err
	}
	if runErr != nil {
		report = ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "runtime: " + runErr.Error(), Policy: report.Policy, Correlation: report.Correlation}
	}
	ended := s.deps.Clock.Now()
	if ended.Before(started) {
		ended = started
	}
	if report.Policy != nil {
		if err := execution.RecordPolicy(*report.Policy); err != nil {
			return err
		}
	}
	if report.Correlation != nil && execution.TraceID() != "" {
		if err := execution.RecordCorrelation(report.Correlation.Carried, report.Correlation.Path); err != nil {
			return err
		}
	}
	if report.Outcome == domain.ExecutionCompleted && report.Result.Answer != "" {
		reference, err := s.deps.Artifacts.SaveReport(ctx, task.ID(), execution.ID(), []byte(report.Result.Answer))
		if err != nil {
			return err
		}
		report.Result.Artifacts = append(report.Result.Artifacts, domain.Artifact{Name: ReportArtifactName, MediaType: ReportMediaType, Reference: reference})
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
	if reference := ReportReference(*execution); reference != "" {
		payload["report"] = reference
	}
	if err := s.rec.executionEvent(ctx, eventType, bot, task.ID(), execution.ID(), task.CorrelationID(), payload); err != nil {
		return err
	}
	auditOutcome := domain.AuditOK
	if execution.Outcome() == domain.ExecutionFailed {
		auditOutcome = domain.AuditFailed
	}
	return s.rec.audit(ctx, action, task.ID().String(), auditOutcome, map[string]string{"execution": execution.ID().String(), "outcome": execution.Outcome().String()})
}

// dropIfEnded keeps the stored execution and task when the execution
// was ended while the run went on (the operator cancelled it): the
// report of a run nobody waits for changes nothing, and is audited.
func (s settler) dropIfEnded(ctx context.Context, action string, task *domain.Task, execution *domain.Execution) (bool, error) {
	stored, err := s.deps.Executions.Get(ctx, execution.ID())
	if err != nil || stored.Outcome() == domain.ExecutionRunning {
		return false, nil
	}
	*execution = stored
	if current, err := s.deps.Tasks.Get(ctx, task.ID()); err == nil {
		*task = current
	}
	details := map[string]string{"execution": execution.ID().String(), "outcome": execution.Outcome().String(), "dropped": "the execution had already ended"}
	return true, s.rec.audit(ctx, action, task.ID().String(), domain.AuditOK, details)
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
