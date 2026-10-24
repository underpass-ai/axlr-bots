package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ApproveTask continues a task that stopped on a tool call a person must
// decide. It hands the decision (approve or deny) to the runtime session
// that keeps the pending call, under a new execution recorded on the
// task. When the stopped execution ran on a node, the session lives
// there: the new execution is assigned to that node and the node reports
// how it ended; otherwise the decision runs in process and is settled
// exactly as RunTask does. Only a WaitingApproval task is accepted, and
// nothing changes until the decision is known good: a wrong word must
// not cost the task its pending call.
type ApproveTask struct {
	deps Dependencies
	rec  recorder
	set  settler
}

// NewApproveTask wires the use case.
func NewApproveTask(deps Dependencies) ApproveTask {
	return ApproveTask{deps: deps, rec: recorder{deps: deps}, set: newSettler(deps)}
}

// Execute decides the pending call and returns the task with the execution
// that carried the decision.
func (uc ApproveTask) Execute(ctx context.Context, id domain.TaskID, decision string) (ApproveTaskResult, error) {
	if !IsDecision(decision) {
		return ApproveTaskResult{}, domain.ValidationError{Field: "decision", Reason: "must be approve or deny"}
	}
	task, err := uc.deps.Tasks.Get(ctx, id)
	if err != nil {
		return ApproveTaskResult{}, err
	}
	if task.Status() != domain.TaskWaitingApproval {
		return ApproveTaskResult{}, uc.deny(ctx, id, domain.StateError{Subject: "task " + id.String(), Current: task.Status().String(), Attempted: "decide"})
	}
	bot, err := uc.deps.Bots.Get(ctx, task.BotID())
	if err != nil {
		return ApproveTaskResult{}, err
	}
	if !bot.AcceptsTasks() {
		return ApproveTaskResult{}, uc.deny(ctx, id, domain.StateError{Subject: "bot " + bot.ID().String(), Current: bot.Status().String(), Attempted: "decide a task"})
	}
	previous, err := uc.deps.Executions.Get(ctx, task.CurrentExecutionID())
	if err != nil {
		return ApproveTaskResult{}, err
	}
	pending := previous.Result().PendingDecision
	if pending == nil {
		return ApproveTaskResult{}, uc.deny(ctx, id, domain.StateError{Subject: "execution " + previous.ID().String(), Current: previous.Outcome().String(), Attempted: "decide a pending call"})
	}
	sessionID := pending.SessionID
	if sessionID == "" {
		sessionID = previous.Result().SessionID
	}

	executionID, err := domain.NewExecutionID(uc.deps.IDs.NewID("exec-"))
	if err != nil {
		return ApproveTaskResult{}, err
	}
	now := uc.deps.Clock.Now()
	if err := task.Resume(executionID, now); err != nil {
		return ApproveTaskResult{}, uc.deny(ctx, id, err)
	}
	execution, err := domain.NewExecution(executionID, task.ID(), bot.ID(), now)
	if err != nil {
		return ApproveTaskResult{}, err
	}
	if err := execution.Correlate(task.CorrelationID()); err != nil {
		return ApproveTaskResult{}, err
	}
	if err := execution.Decide(decision); err != nil {
		return ApproveTaskResult{}, err
	}
	if previous.IsAssigned() {
		if err := execution.AssignTo(previous.NodeID()); err != nil {
			return ApproveTaskResult{}, err
		}
	}
	if err := uc.deps.Executions.Save(ctx, execution); err != nil {
		return ApproveTaskResult{}, err
	}
	if err := uc.deps.Tasks.Save(ctx, task); err != nil {
		return ApproveTaskResult{}, err
	}
	payload := map[string]any{"decision": decision, "callId": pending.CallID, "sessionId": sessionID, "executionId": executionID.String()}
	if previous.IsAssigned() {
		payload["nodeId"] = previous.NodeID().String()
	}
	if err := uc.rec.executionEvent(ctx, domain.EventTaskDecided, bot, task.ID(), executionID, task.CorrelationID(), payload); err != nil {
		return ApproveTaskResult{}, err
	}
	if previous.IsAssigned() {
		// The node takes it on its next poll (NextAssignment) and reports.
		if err := uc.rec.audit(ctx, "task.decide", task.ID().String(), domain.AuditOK, map[string]string{"execution": executionID.String(), "queuedFor": previous.NodeID().String()}); err != nil {
			return ApproveTaskResult{}, err
		}
		return ApproveTaskResult{Task: task, Execution: execution}, nil
	}

	report, runErr := uc.deps.Decisions.Decide(ctx, DecisionRequest{
		ExecutionID:  executionID,
		BotID:        bot.ID(),
		SessionID:    sessionID,
		CallID:       pending.CallID,
		Decision:     decision,
		Level:        bot.Level(),
		AllowedTools: bot.AllowedTools().Names(),
		ToolRules:    bot.ToolRules(),
		Timeout:      bot.Budgets().TaskTimeoutOr(DefaultTaskTimeout),
	})
	if err := uc.set.settle(ctx, "task.decide", bot, &task, &execution, report, runErr, now); err != nil {
		return ApproveTaskResult{}, err
	}
	return ApproveTaskResult{Task: task, Execution: execution}, nil
}

// deny audits a refused decision and returns its cause.
func (uc ApproveTask) deny(ctx context.Context, id domain.TaskID, cause error) error {
	_ = uc.rec.audit(ctx, "task.decide", id.String(), domain.AuditDenied, map[string]string{"reason": cause.Error()})
	return cause
}
