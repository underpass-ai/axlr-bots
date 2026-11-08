package application

import (
	"context"
	"strconv"
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

// Execute decides the pending call as a person and returns the task with
// the execution that carried the decision. callID, when given, must name
// the pending call: an approve link for one call never decides another.
// actor is who decides, as the token holder asserts it (the CLI's
// --actor, the panel's operator.actor, X-Axlr-Actor or decidedBy on the
// API); empty is the control plane's own actor, as before approvers
// existed, except on a bot that lists approvers or for a call that waits
// for two persons, which take a named actor. An actor the bot's
// approvers do not list is refused (domain.ForbiddenError, audited
// approval.refused), on approve and deny alike. A call that waits for
// two persons records its first approval (bot.approval.first) and runs
// nothing; the second, by another actor, runs it; the same actor again
// is refused; a denial ends the wait (docs/architecture/approvers.md).
func (uc ApproveTask) Execute(ctx context.Context, id domain.TaskID, decision, callID, actor string) (ApproveTaskResult, error) {
	return uc.decide(ctx, id, decision, callID, decider{name: actor, person: true})
}

// byPolicy approves the pending call as the control plane's actor, a
// policy (autoApprove, the team's events) and not a person: approvers do
// not apply to it, and the settler never hands it a call that waits for
// two persons.
func (uc ApproveTask) byPolicy(ctx context.Context, id domain.TaskID, callID string) (ApproveTaskResult, error) {
	return uc.decide(ctx, id, DecisionApprove, callID, decider{name: uc.deps.Actor})
}

// decider is who takes a decision: the name it is taken under, and
// whether it is a person's (checked against the bot's approvers) or a
// policy's.
type decider struct {
	name   string
	person bool
	// attested is a person who named themselves: the only decision the
	// autonomy ladder counts. A daemon's own name is not one.
	attested bool
}

func (uc ApproveTask) decide(ctx context.Context, id domain.TaskID, decision, callID string, by decider) (ApproveTaskResult, error) {
	if !IsDecision(decision) {
		return ApproveTaskResult{}, domain.ValidationError{Field: "decision", Reason: "must be approve or deny"}
	}
	own := uc.deps.Actor
	if by.name != "" {
		name, err := domain.ValidateActor("decidedBy", by.name)
		if err != nil {
			return ApproveTaskResult{}, err
		}
		if by.person && strings.HasPrefix(name, domain.PolicyActorPrefix) {
			// A person never decides as a policy (review of #139).
			return ApproveTaskResult{}, uc.refuse(ctx, id, decision, name, domain.ForbiddenError{Reason: name + " is a policy's name: a person decides under their own"})
		}
		if name != uc.deps.Actor {
			// The asserted actor is who the audit, the events and the
			// incident record, as settler.autoApprove does for a policy.
			deps := uc.deps
			deps.Actor = name
			uc = NewApproveTask(deps)
		}
		by.name = name
		by.attested = by.person && personName(Dependencies{Daemon: uc.deps.Daemon, Actor: own}, name)
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
	if decision == DecisionApprove {
		// A task of an incident is approved only while the incident stands
		// on it; a deny only lets its session go on, and stays allowed.
		stands, incident, err := incidentStanding{deps: uc.deps}.standsOn(ctx, task)
		if err != nil {
			return ApproveTaskResult{}, err
		}
		if !stands {
			current := "a task incident " + incident.ID().String() + " no longer stands on (" + incident.Status().String() + ", cycle " + strconv.Itoa(incident.Cycle()) + ")"
			return ApproveTaskResult{}, uc.deny(ctx, id, domain.StateError{Subject: "task " + id.String(), Current: current, Attempted: "approve"})
		}
	}
	bot, err := uc.deps.Bots.Get(ctx, task.BotID())
	if err != nil {
		return ApproveTaskResult{}, err
	}
	needed := uc.needed(ctx, bot, &task)
	if by.person {
		if err := uc.mayDecide(ctx, bot, task, decision, by.name, own, needed); err != nil {
			return ApproveTaskResult{}, err
		}
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
	if _, gone, _ := bot.PolicyOf(task); gone && decision == DecisionApprove {
		// The incident trigger the task's policy was pinned by is gone:
		// nothing says what the call may run under now (fail closed).
		return ApproveTaskResult{}, uc.deny(ctx, id, domain.StateError{Subject: "task " + id.String(), Current: "pinned to trigger " + task.Trigger().Name + ", which bot " + bot.ID().String() + " no longer has", Attempted: "approve"})
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
	// Only a person's approval counts as one of two: a policy decider
	// (the team's events) resumes the call outright, and an autoApprove
	// rule never gets here for a call that waits for two (settler).
	if by.person && decision == DecisionApprove && needed > 1 {
		wait, _ := task.Approval()
		if wait.ApprovedByActor(uc.deps.Actor) {
			return ApproveTaskResult{}, uc.refuse(ctx, task.ID(), decision, uc.deps.Actor, domain.StateError{Subject: "task " + id.String(), Current: "already approved by " + uc.deps.Actor + ", waiting for a second, different person", Attempted: "approve again as the same person"})
		}
		if len(wait.ApprovedBy)+1 < needed {
			return uc.first(ctx, bot, task, previous)
		}
	}
	return uc.resume(ctx, bot, task, previous, decision, outcome, by, nil)
}

// needed is how many people the task's open wait needs: what it was
// opened with, raised to what the bot and the incident say now (a bot
// that asks for two persons since the wait began gets them).
func (uc ApproveTask) needed(ctx context.Context, bot domain.Bot, task *domain.Task) int {
	wait, ok := task.Approval()
	if !ok || !wait.Open() {
		return 1
	}
	needed := wait.Needed()
	if now := approvalsNeeded(ctx, uc.deps, bot, *task); now > needed {
		if task.RaiseApprovalsNeeded(now) == nil {
			needed = now
		}
	}
	return needed
}

// mayDecide refuses a person's decision the bot's approvals do not let
// them take: an actor its approvers do not list, no actor at all on a
// bot that lists approvers, or an approval without a named actor of a
// call that waits for two persons (two names are what tell two people
// apart). Each refusal is audited approval.refused with the actor.
func (uc ApproveTask) mayDecide(ctx context.Context, bot domain.Bot, task domain.Task, decision, asserted, own string, needed int) error {
	approvals := bot.Approvals()
	names := len(approvals.Approvers()) > 0 || needed > 1
	switch {
	case names && uc.deps.Daemon && asserted != "" && domain.ActorKey(asserted) == domain.ActorKey(own):
		return uc.refuse(ctx, task.ID(), decision, asserted, domain.ForbiddenError{Reason: asserted + " is the control plane's own name: a person decides under their own"})
	case asserted == "" && len(approvals.Approvers()) > 0:
		return uc.refuse(ctx, task.ID(), decision, "", domain.ForbiddenError{Reason: "bot " + bot.ID().String() + " lists who may decide its calls: name who decides (--actor, the panel's operator.actor, X-Axlr-Actor or decidedBy)"})
	case asserted == "" && needed > 1 && decision == DecisionApprove:
		return uc.refuse(ctx, task.ID(), decision, "", domain.ForbiddenError{Reason: "task " + task.ID().String() + " waits for two persons: name who approves (--actor, the panel's operator.actor, X-Axlr-Actor or decidedBy)"})
	case asserted != "" && !approvals.MayDecide(asserted):
		return uc.refuse(ctx, task.ID(), decision, asserted, domain.ForbiddenError{Reason: asserted + " is not among the approvers of bot " + bot.ID().String()})
	}
	return nil
}

// refuse audits a decision the approvals refused, with the actor that
// asked, and returns its cause.
func (uc ApproveTask) refuse(ctx context.Context, id domain.TaskID, decision, actor string, cause error) error {
	_ = uc.rec.audit(ctx, "approval.refused", id.String(), domain.AuditDenied, map[string]string{"actor": actor, "decision": decision, "reason": cause.Error()})
	return cause
}

// first records the first of the two approvals a call waits for: the
// task keeps waiting under the same deadline and nothing runs. It is
// told as bot.approval.first on the stopped execution and audited
// approvals.first with the actor and the time.
func (uc ApproveTask) first(ctx context.Context, bot domain.Bot, task domain.Task, previous domain.Execution) (ApproveTaskResult, error) {
	now := uc.deps.Clock.Now()
	if err := task.AddApproval(uc.deps.Actor, now); err != nil {
		return ApproveTaskResult{}, uc.refuse(ctx, task.ID(), DecisionApprove, uc.deps.Actor, err)
	}
	if err := uc.deps.Tasks.Save(ctx, task); err != nil {
		return ApproveTaskResult{}, err
	}
	wait, _ := task.Approval()
	payload := map[string]any{"callId": wait.CallID, "tool": wait.Tool, "approvedBy": uc.deps.Actor, "at": now, "approvalsNeeded": wait.Needed(), "approvals": len(wait.ApprovedBy), "deadline": wait.Deadline}
	if err := uc.rec.executionEvent(ctx, domain.EventApprovalFirst, bot, task.ID(), previous.ID(), task.CorrelationID(), payload); err != nil {
		return ApproveTaskResult{}, err
	}
	details := map[string]string{"actor": uc.deps.Actor, "at": now.UTC().Format(time.RFC3339), "call": wait.CallID, "approvals": strconv.Itoa(len(wait.ApprovedBy)) + " of " + strconv.Itoa(wait.Needed())}
	if err := uc.rec.audit(ctx, "approvals.first", task.ID().String(), domain.AuditOK, details); err != nil {
		return ApproveTaskResult{}, err
	}
	return ApproveTaskResult{Task: task, Execution: previous}, nil
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
func (uc ApproveTask) resume(ctx context.Context, bot domain.Bot, task domain.Task, previous domain.Execution, decision, outcome string, by decider, extra map[string]any) (ApproveTaskResult, error) {
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
	if outcome == domain.ApprovalApproved && by.person {
		// A call that waits for two keeps both names on its closed wait.
		if err := task.FinalApproval(uc.deps.Actor, now); err != nil {
			return ApproveTaskResult{}, uc.deny(ctx, task.ID(), err)
		}
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
	if wait, ok := task.Approval(); ok && wait.Needed() > 1 && outcome == domain.ApprovalApproved {
		approvers := []string{}
		for _, mark := range wait.ApprovedBy {
			approvers = append(approvers, mark.Actor)
		}
		payload["approvedBy"], payload["approvalsNeeded"] = approvers, wait.Needed()
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
	policy, _, policyErr := bot.PolicyOf(task)
	if policyErr != nil {
		// Nothing of the task's policy is left to run the decision under:
		// an empty allow-list would let the level alone decide. The task
		// fails with the reason, in process and for a node alike.
		failed := ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "policy refused: " + policyErr.Error()}
		if err := uc.set.settle(ctx, "task.decide", bot, &task, &execution, failed, nil, now); err != nil {
			return ApproveTaskResult{}, err
		}
		return ApproveTaskResult{Task: task, Execution: execution}, nil
	}
	// The decision of an incident's apply call moves the incident now,
	// before the call runs: approved is Applying, denied or expired is
	// back to Open (design §4).
	newIncidentAdvancer(uc.deps).taskDecided(ctx, task, outcome, pending.CallID, pending.Tool, uc.deps.Actor, by.attested)
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
		Level:         policy.Level(),
		AllowedTools:  policy.Tools().Names(),
		ToolRules:     policy.Rules(),
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
