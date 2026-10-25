package application

import (
	"context"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// settler records how a run ended: it applies the runtime's report to the
// execution and the task, persists both, publishes the outcome event and
// audits the action. RunTask and ApproveTask end their runs through it.
// A run that stops on a call gives the task its approval deadline and
// queues the bot's notifications; the artifacts the run delivered are
// marked as kept or not, and read from the workspace when the control
// plane ran it itself.
type settler struct {
	deps   Dependencies
	rec    recorder
	keeper artifactKeeper
	notes  approvalNotifier
}

func newSettler(deps Dependencies) settler {
	rec := recorder{deps: deps}
	return settler{deps: deps, rec: rec, keeper: artifactKeeper{deps: deps, rec: rec}, notes: approvalNotifier{deps: deps, rec: rec}}
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
	details := map[string]string{"execution": execution.ID().String()}
	if err := execution.RecordReceipt(report.Receipt); err != nil {
		details["receipt"] = "dropped: " + err.Error()
	}
	if !execution.IsAssigned() {
		s.keeper.collect(ctx, bot, *execution, report.Result.Artifacts)
	}
	report.Result.Artifacts = s.keeper.mark(ctx, execution.ID(), report.Result.Artifacts)
	if report.Outcome == domain.ExecutionCompleted && report.Result.Answer != "" {
		reference, err := s.deps.Artifacts.SaveReport(ctx, task.ID(), execution.ID(), []byte(report.Result.Answer))
		if err != nil {
			return err
		}
		report.Result.Artifacts = append(report.Result.Artifacts, domain.Artifact{Name: ReportArtifactName, MediaType: ReportMediaType, Reference: reference, Source: domain.ArtifactSourceAnswer})
	}
	eventType, err := apply(task, execution, report, bot.Approvals(), ended)
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
	wait, waiting := task.Approval()
	waiting = waiting && wait.Open() && task.Status() == domain.TaskWaitingApproval
	if waiting {
		payload["callId"], payload["tool"], payload["deadline"], payload["onExpire"] = wait.CallID, wait.Tool, wait.Deadline, wait.OnExpire.String()
	}
	if stored := storedNames(execution.Result().Artifacts); len(stored) > 0 {
		payload["artifacts"] = stored
	}
	if err := s.rec.executionEvent(ctx, eventType, bot, task.ID(), execution.ID(), task.CorrelationID(), payload); err != nil {
		return err
	}
	auditOutcome := domain.AuditOK
	if execution.Outcome() == domain.ExecutionFailed {
		auditOutcome = domain.AuditFailed
	}
	details["outcome"] = execution.Outcome().String()
	if err := s.rec.audit(ctx, action, task.ID().String(), auditOutcome, details); err != nil {
		return err
	}
	if waiting {
		return s.notes.queue(ctx, bot, *task, *execution, wait)
	}
	return nil
}

// storedNames are the names of the artifacts the store keeps.
func storedNames(artifacts []domain.Artifact) []string {
	var names []string
	for _, artifact := range artifacts {
		if artifact.Stored {
			names = append(names, artifact.Name)
		}
	}
	return names
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
// names the event that tells it. A task that stops on a call waits for
// the bot's approval time to live.
func apply(task *domain.Task, execution *domain.Execution, report ExecutionReport, approvals domain.Approvals, ended time.Time) (domain.EventType, error) {
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
		pending := report.Result.PendingDecision
		wait := domain.ApprovalWait{CallID: pending.CallID, Tool: pending.Tool, Since: ended, Deadline: ended.Add(approvals.TTL()), OnExpire: approvals.OnExpire()}
		return domain.EventTaskWaitingApproval, task.AwaitApproval(wait, ended)
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
