package application

import (
	"context"
	"time"

	"github.com/underpass-ai/underpass-bots/domain"
)

// DefaultTaskTimeout bounds a task whose bot sets no timeout.
const DefaultTaskTimeout = 15 * time.Minute

// RunTask runs a Pending task once through the runtime: it persists the
// task as Running with its execution before calling the runtime, so a
// crash leaves a visible Running task rather than a lost one, then records
// the outcome. A task that is not Pending is refused: no second execution.
type RunTask struct {
	deps Dependencies
	rec  recorder
}

// NewRunTask wires the use case.
func NewRunTask(deps Dependencies) RunTask {
	return RunTask{deps: deps, rec: recorder{deps: deps}}
}

// Execute runs the task and returns it with its execution.
func (uc RunTask) Execute(ctx context.Context, id domain.TaskID) (RunTaskResult, error) {
	task, err := uc.deps.Tasks.Get(ctx, id)
	if err != nil {
		return RunTaskResult{}, err
	}
	bot, err := uc.deps.Bots.Get(ctx, task.BotID())
	if err != nil {
		return RunTaskResult{}, err
	}
	if !bot.AcceptsTasks() {
		_ = uc.rec.audit(ctx, "task.run", id.String(), domain.AuditDenied, map[string]string{"reason": "bot is " + bot.Status().String()})
		return RunTaskResult{}, domain.StateError{Subject: "bot " + bot.ID().String(), Current: bot.Status().String(), Attempted: "run a task"}
	}
	executionID, err := domain.NewExecutionID(uc.deps.IDs.NewID("exec-"))
	if err != nil {
		return RunTaskResult{}, err
	}
	now := uc.deps.Clock.Now()
	if err := task.Start(executionID, now); err != nil {
		_ = uc.rec.audit(ctx, "task.run", id.String(), domain.AuditDenied, map[string]string{"reason": err.Error()})
		return RunTaskResult{}, err
	}
	execution, err := domain.NewExecution(executionID, task.ID(), bot.ID(), now)
	if err != nil {
		return RunTaskResult{}, err
	}
	if err := uc.deps.Executions.Save(ctx, execution); err != nil {
		return RunTaskResult{}, err
	}
	if err := uc.deps.Tasks.Save(ctx, task); err != nil {
		return RunTaskResult{}, err
	}
	if err := uc.rec.event(ctx, domain.EventTaskStarted, bot, task.ID(), task.CorrelationID(), map[string]any{"executionId": executionID.String()}); err != nil {
		return RunTaskResult{}, err
	}

	report, runErr := uc.deps.Runtime.Execute(ctx, ExecutionRequest{
		ExecutionID:   executionID,
		TaskID:        task.ID(),
		BotID:         bot.ID(),
		TenantID:      bot.TenantID(),
		CorrelationID: task.CorrelationID(),
		Objective:     task.Objective(),
		Model:         bot.ModelPolicy().Model(),
		AllowedTools:  bot.AllowedTools().Names(),
		Budgets:       bot.Budgets(),
		Timeout:       bot.Budgets().TaskTimeoutOr(DefaultTaskTimeout),
	})
	if runErr != nil {
		report = ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "runtime: " + runErr.Error()}
	}
	ended := uc.deps.Clock.Now()
	if ended.Before(now) {
		ended = now
	}
	eventType, err := uc.settle(&task, &execution, report, ended)
	if err != nil {
		return RunTaskResult{}, err
	}
	if err := uc.deps.Executions.Save(ctx, execution); err != nil {
		return RunTaskResult{}, err
	}
	if err := uc.deps.Tasks.Save(ctx, task); err != nil {
		return RunTaskResult{}, err
	}
	payload := map[string]any{"executionId": executionID.String(), "outcome": execution.Outcome().String()}
	if execution.FailureReason() != "" {
		payload["reason"] = execution.FailureReason()
	}
	if err := uc.rec.event(ctx, eventType, bot, task.ID(), task.CorrelationID(), payload); err != nil {
		return RunTaskResult{}, err
	}
	auditOutcome := domain.AuditOK
	if execution.Outcome() == domain.ExecutionFailed {
		auditOutcome = domain.AuditFailed
	}
	if err := uc.rec.audit(ctx, "task.run", id.String(), auditOutcome, map[string]string{"execution": executionID.String(), "outcome": execution.Outcome().String()}); err != nil {
		return RunTaskResult{}, err
	}
	return RunTaskResult{Task: task, Execution: execution}, nil
}

// settle applies the report to the execution and the task.
func (uc RunTask) settle(task *domain.Task, execution *domain.Execution, report ExecutionReport, ended time.Time) (domain.EventType, error) {
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
