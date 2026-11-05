package application

import (
	"context"
	"strings"
	"time"

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
// not cost the task its pending call. A decision after the call's
// deadline authorises nothing (vision §14): it is refused with a
// domain.ApprovalExpiredError, whether the expiry was carried out yet or
// not, and so is one that names another call than the pending one.
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
// that carried the decision. callID, when given, must name the pending
// call: an approve link for one call never decides another.
func (uc ApproveTask) Execute(ctx context.Context, id domain.TaskID, decision, callID string) (ApproveTaskResult, error) {
	if !IsDecision(decision) {
		return ApproveTaskResult{}, domain.ValidationError{Field: "decision", Reason: "must be approve or deny"}
	}
	task, err := uc.deps.Tasks.Get(ctx, id)
	if err != nil {
		return ApproveTaskResult{}, err
	}
	if wait, ok := task.Approval(); ok && wait.Outcome == domain.ApprovalExpired && (callID == "" || callID == wait.CallID) {
		return ApproveTaskResult{}, uc.deny(ctx, id, domain.ApprovalExpiredError{TaskID: id.String(), CallID: wait.CallID, Deadline: wait.Deadline})
	}
	if task.Status() != domain.TaskWaitingApproval {
		return ApproveTaskResult{}, uc.deny(ctx, id, domain.StateError{Subject: "task " + id.String(), Current: task.Status().String(), Attempted: "decide"})
	}
	bot, err := uc.deps.Bots.Get(ctx, task.BotID())
	if err != nil {
		return ApproveTaskResult{}, err
	}
	if task.DeadlinePassed(uc.deps.Clock.Now()) {
		// The delegation's deadline came while the task waited: nothing
		// it was granted is left to run the call with.
		if err := newDeadlineKeeper(uc.deps).miss(ctx, bot, &task, nil); err != nil {
			return ApproveTaskResult{}, err
		}
		return ApproveTaskResult{}, uc.deny(ctx, id, domain.StateError{Subject: "task " + id.String(), Current: "past its delegation's deadline", Attempted: "decide"})
	}
	previous, err := uc.deps.Executions.Get(ctx, task.CurrentExecutionID())
	if err != nil {
		return ApproveTaskResult{}, err
	}
	pending := previous.Result().PendingDecision
	if deadline, expired := expiredWait(task, bot, uc.deps.Clock.Now()); expired {
		call := ""
		if pending != nil {
			call = pending.CallID
		}
		return ApproveTaskResult{}, uc.deny(ctx, id, domain.ApprovalExpiredError{TaskID: id.String(), CallID: call, Deadline: deadline})
	}
	if !bot.AcceptsTasks() {
		return ApproveTaskResult{}, uc.deny(ctx, id, domain.StateError{Subject: "bot " + bot.ID().String(), Current: bot.Status().String(), Attempted: "decide a task"})
	}
	if pending == nil {
		return ApproveTaskResult{}, uc.deny(ctx, id, domain.StateError{Subject: "execution " + previous.ID().String(), Current: previous.Outcome().String(), Attempted: "decide a pending call"})
	}
	if callID != "" && callID != pending.CallID {
		return ApproveTaskResult{}, uc.deny(ctx, id, domain.StateError{Subject: "task " + id.String(), Current: "waiting on call " + pending.CallID, Attempted: "decide call " + callID})
	}
	outcome := domain.ApprovalApproved
	if decision == DecisionDeny {
		outcome = domain.ApprovalDenied
	}
	return uc.resume(ctx, bot, task, previous, decision, outcome, nil)
}

// expiredWait is the deadline the task's pending call had to be decided
// by and whether it passed: the wait's, or for a task that waited before
// approvals had deadlines, the bot's time to live from when it began to
// wait. A wait the expiry already closed has passed.
func expiredWait(task domain.Task, bot domain.Bot, now time.Time) (time.Time, bool) {
	wait, ok := task.Approval()
	switch {
	case ok && wait.Outcome == domain.ApprovalExpired:
		return wait.Deadline, true
	case ok && wait.Open():
		return wait.Deadline, now.After(wait.Deadline)
	}
	deadline := task.UpdatedAt().Add(bot.Approvals().TTL())
	return deadline, now.After(deadline)
}

// resume hands the decision to the session under a new execution and
// closes the wait with the outcome (approved, denied, or expired when the
// deadline denied the call). extra joins the decided event's payload.
func (uc ApproveTask) resume(ctx context.Context, bot domain.Bot, task domain.Task, previous domain.Execution, decision, outcome string, extra map[string]any) (ApproveTaskResult, error) {
	pending := previous.Result().PendingDecision
	sessionID := pending.SessionID
	if sessionID == "" {
		sessionID = previous.Result().SessionID
	}

	executionID, err := domain.NewExecutionID(uc.deps.IDs.NewID("exec-"))
	if err != nil {
		return ApproveTaskResult{}, err
	}
	now := uc.deps.Clock.Now()
	timeout, inTime := runTimeout(bot, task, now)
	if !inTime {
		// Less than a second of the delegation is left: the call is not
		// handed over to run without a timeout.
		if err := newDeadlineKeeper(uc.deps).miss(ctx, bot, &task, nil); err != nil {
			return ApproveTaskResult{}, err
		}
		return ApproveTaskResult{}, uc.deny(ctx, task.ID(), domain.StateError{Subject: "task " + task.ID().String(), Current: "past its delegation's deadline", Attempted: "decide"})
	}
	if err := task.Resume(executionID, outcome, now); err != nil {
		return ApproveTaskResult{}, uc.deny(ctx, task.ID(), err)
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
	waits := map[string]any{}
	if previous.IsAssigned() {
		if err := execution.AssignTo(previous.NodeID()); err != nil {
			return ApproveTaskResult{}, err
		}
		// Read before anything is saved: a node store that fails then
		// leaves the task waiting for its decision.
		if err := uc.waitingFor(ctx, previous.NodeID(), task.ID(), waits); err != nil {
			return ApproveTaskResult{}, err
		}
	}
	if err := uc.deps.Executions.Save(ctx, execution); err != nil {
		return ApproveTaskResult{}, err
	}
	if err := uc.deps.Tasks.Save(ctx, task); err != nil {
		return ApproveTaskResult{}, err
	}
	payload := map[string]any{"decision": decision, "callId": pending.CallID, "sessionId": sessionID, "executionId": executionID.String(), "decidedBy": uc.deps.Actor}
	if previous.IsAssigned() {
		payload["nodeId"] = previous.NodeID().String()
	}
	for key, value := range waits {
		payload[key] = value
	}
	for key, value := range extra {
		payload[key] = value
	}
	if err := uc.rec.executionEvent(ctx, domain.EventTaskDecided, bot, task.ID(), executionID, task.CorrelationID(), payload); err != nil {
		return ApproveTaskResult{}, err
	}
	if previous.IsAssigned() {
		// The node takes it on its next poll (NextAssignment) and reports.
		details := map[string]string{"execution": executionID.String(), "queuedFor": previous.NodeID().String(), "decision": decision}
		if waiting, ok := payload["waiting"].(string); ok {
			details["waiting"] = waiting
		}
		if err := uc.rec.audit(ctx, "task.decide", task.ID().String(), domain.AuditOK, details); err != nil {
			return ApproveTaskResult{}, err
		}
		return ApproveTaskResult{Task: task, Execution: execution}, nil
	}

	report, runErr := uc.deps.Decisions.Decide(ctx, DecisionRequest{
		ExecutionID:   executionID,
		TaskID:        task.ID(),
		BotID:         bot.ID(),
		CorrelationID: task.CorrelationID(),
		SessionID:     sessionID,
		CallID:        pending.CallID,
		Decision:      decision,
		Level:         bot.Level(),
		AllowedTools:  bot.AllowedTools().Names(),
		ToolRules:     bot.ToolRules(),
		Timeout:       timeout,
	})
	if err := uc.set.settle(ctx, "task.decide", bot, &task, &execution, report, runErr, now); err != nil {
		return ApproveTaskResult{}, err
	}
	return ApproveTaskResult{Task: task, Execution: execution}, nil
}

// waitingFor says, in the decided event, what the queued decision waits
// for when its node cannot take it now: the node carries one execution
// at a time and polls only when it is free, so a decision queued for a
// node busy with a long run waits for that run's end, and one queued
// for an Offline node waits for it to come back (reconcile fails it once
// the node is unseen past the grace). Before, the task showed Running
// with no hint (QA, 5 November 2026). busyWith lists the tasks the node
// runs; waiting says it in words. Nothing is added for a free node.
func (uc ApproveTask) waitingFor(ctx context.Context, nodeID domain.NodeID, own domain.TaskID, payload map[string]any) error {
	node, err := uc.deps.Nodes.Get(ctx, nodeID)
	if domain.IsNotFound(err) {
		payload["waiting"] = "waiting for node " + nodeID.String() + ", which is gone"
		return nil
	}
	if err != nil {
		return err
	}
	if !node.IsOnline() {
		payload["nodeStatus"] = node.Status().String()
		payload["waiting"] = "waiting for node " + nodeID.String() + ", which is " + node.Status().String()
		return nil
	}
	running, err := uc.deps.Executions.ListRunning(ctx)
	if err != nil {
		return err
	}
	busy := []string{}
	for _, execution := range running {
		if execution.NodeID() == nodeID && execution.TaskID() != own && !execution.LeaseUntil().IsZero() {
			busy = append(busy, execution.TaskID().String())
		}
	}
	if len(busy) > 0 {
		payload["busyWith"] = busy
		payload["waiting"] = "waiting for node " + nodeID.String() + ", busy with task " + strings.Join(busy, ", ")
	}
	return nil
}

// deny audits a refused decision and returns its cause.
func (uc ApproveTask) deny(ctx context.Context, id domain.TaskID, cause error) error {
	_ = uc.rec.audit(ctx, "task.decide", id.String(), domain.AuditDenied, map[string]string{"reason": cause.Error()})
	return cause
}
