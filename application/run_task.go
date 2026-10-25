package application

import (
	"context"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
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
	set  settler
}

// NewRunTask wires the use case.
func NewRunTask(deps Dependencies) RunTask {
	return RunTask{deps: deps, rec: recorder{deps: deps}, set: newSettler(deps)}
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
	if err := execution.Correlate(task.CorrelationID()); err != nil {
		return RunTaskResult{}, err
	}
	if err := uc.deps.Executions.Save(ctx, execution); err != nil {
		return RunTaskResult{}, err
	}
	if err := uc.deps.Tasks.Save(ctx, task); err != nil {
		return RunTaskResult{}, err
	}
	if err := uc.rec.executionEvent(ctx, domain.EventTaskStarted, bot, task.ID(), executionID, task.CorrelationID(), map[string]any{"executionId": executionID.String()}); err != nil {
		return RunTaskResult{}, err
	}

	prompt, err := domain.NewPrompt(bot.Mission(), task.Objective())
	if err != nil {
		return RunTaskResult{}, err
	}
	report, runErr := uc.deps.Runtime.Execute(ctx, ExecutionRequest{
		ExecutionID:   executionID,
		TaskID:        task.ID(),
		BotID:         bot.ID(),
		TenantID:      bot.TenantID(),
		CorrelationID: task.CorrelationID(),
		Objective:     task.Objective(),
		Prompt:        prompt,
		Model:         bot.ModelPolicy().Model(),
		Level:         bot.Level(),
		AllowedTools:  bot.AllowedTools().Names(),
		ToolRules:     bot.ToolRules(),
		Budgets:       bot.Budgets(),
		Timeout:       bot.Budgets().TaskTimeoutOr(DefaultTaskTimeout),
	})
	if err := uc.set.settle(ctx, "task.run", bot, &task, &execution, report, runErr, now); err != nil {
		return RunTaskResult{}, err
	}
	return RunTaskResult{Task: task, Execution: execution}, nil
}
