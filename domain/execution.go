package domain

import "time"

// Execution is one attempt at a task: when it ran, how it ended and what
// it produced. The control plane reads its outcome and result from the
// runtime contract; the trace file is evidence, not the result.
type Execution struct {
	id            ExecutionID
	taskID        TaskID
	botID         BotID
	startedAt     time.Time
	endedAt       time.Time
	outcome       ExecutionOutcome
	result        Result
	usage         Usage
	failureReason string
	tracePath     string
	policy        PolicyRecord
	correlation   CorrelationRecord
	kind          ExecutionKind
	decision      string
	nodeID        NodeID
	leaseUntil    time.Time
}

// NewExecution starts an attempt at the task.
func NewExecution(id ExecutionID, task TaskID, bot BotID, startedAt time.Time) (Execution, error) {
	if startedAt.IsZero() {
		return Execution{}, invalid("started at", "must not be zero")
	}
	return Execution{id: id, taskID: task, botID: bot, startedAt: startedAt, outcome: ExecutionRunning, kind: ExecutionRun}, nil
}

// Kind is what the execution carries: a run or a decision.
func (e Execution) Kind() ExecutionKind { return e.kind }

// NodeID is the node the execution was assigned to; empty when the
// control plane ran it in process.
func (e Execution) NodeID() NodeID { return e.nodeID }

// LeaseUntil is when the node's lease on the execution expires; zero
// while the execution waits for the node to take it, or in process.
func (e Execution) LeaseUntil() time.Time { return e.leaseUntil }

// IsAssigned reports an execution a node is to run.
func (e Execution) IsAssigned() bool { return e.nodeID != "" }

// LeaseExpired reports a leased execution the node stopped renewing.
func (e Execution) LeaseExpired(now time.Time) bool {
	return e.outcome == ExecutionRunning && !e.leaseUntil.IsZero() && now.After(e.leaseUntil)
}

// Decision is the word a decide execution carries: approve or deny.
func (e Execution) Decision() string { return e.decision }

// Decide makes the execution carry a decision instead of a run.
func (e *Execution) Decide(decision string) error {
	if e.outcome != ExecutionRunning {
		return StateError{Subject: "execution " + e.id.String(), Current: e.outcome.String(), Attempted: "carry a decision"}
	}
	if decision == "" {
		return invalid("decision", "must not be empty")
	}
	e.kind = ExecutionDecide
	e.decision = decision
	return nil
}

// AssignTo hands a running execution to a node, unleased until the node
// takes it.
func (e *Execution) AssignTo(node NodeID) error {
	if e.outcome != ExecutionRunning {
		return StateError{Subject: "execution " + e.id.String(), Current: e.outcome.String(), Attempted: "assign"}
	}
	if node == "" {
		return invalid("node id", "must not be empty")
	}
	e.nodeID = node
	e.leaseUntil = time.Time{}
	return nil
}

// Lease gives the assigned node the execution until the time; renewing
// extends it.
func (e *Execution) Lease(until time.Time) error {
	if e.outcome != ExecutionRunning || e.nodeID == "" {
		return StateError{Subject: "execution " + e.id.String(), Current: e.outcome.String(), Attempted: "lease"}
	}
	if until.Before(e.startedAt) {
		return invalid("lease until", "must not be before started at")
	}
	e.leaseUntil = until
	return nil
}

// ID is the execution's identifier.
func (e Execution) ID() ExecutionID { return e.id }

// TaskID is the task this attempt belongs to.
func (e Execution) TaskID() TaskID { return e.taskID }

// BotID is the bot that ran.
func (e Execution) BotID() BotID { return e.botID }

// StartedAt is when the attempt began.
func (e Execution) StartedAt() time.Time { return e.startedAt }

// EndedAt is when the attempt ended; zero while running.
func (e Execution) EndedAt() time.Time { return e.endedAt }

// Outcome is how the attempt ended, or Running.
func (e Execution) Outcome() ExecutionOutcome { return e.outcome }

// Result is what the attempt produced.
func (e Execution) Result() Result { return e.result }

// Usage is what the attempt consumed.
func (e Execution) Usage() Usage { return e.usage }

// FailureReason says why the attempt failed, when it did.
func (e Execution) FailureReason() string { return e.failureReason }

// TracePath is where the raw runtime events were kept, if anywhere.
func (e Execution) TracePath() string { return e.tracePath }

// Policy says under which policy the attempt ran; zero until recorded.
func (e Execution) Policy() PolicyRecord { return e.policy }

// Correlation is the execution's ids as the runtime contract names
// them; zero until Correlate.
func (e Execution) Correlation() CorrelationRecord { return e.correlation }

// TraceID is the task's trace id the execution carries.
func (e Execution) TraceID() CorrelationID { return CorrelationID(e.correlation.TraceID) }

// Correlate gives the execution the task's trace id: with its own ids
// that is the correlation the runtime receives.
func (e *Execution) Correlate(trace CorrelationID) error {
	if trace == "" {
		return invalid("trace id", "must not be empty")
	}
	e.correlation = CorrelationRecord{BotID: e.botID.String(), TaskID: e.taskID.String(), ExecutionID: e.id.String(), TraceID: trace.String()}
	return nil
}

// RecordCorrelation keeps what the runtime adapter did with the
// correlation: whether it passed it to AXLR and the file it wrote.
func (e *Execution) RecordCorrelation(carried bool, path string) error {
	if e.correlation.TraceID == "" {
		return invalid("correlation", "the execution has no trace id")
	}
	e.correlation.Carried, e.correlation.Path = carried, path
	return nil
}

// RecordPolicy keeps the policy the runtime adapter ran the attempt
// under: the levels, whether the runtime enforced it and the file.
func (e *Execution) RecordPolicy(record PolicyRecord) error {
	if record.Level == "" {
		return invalid("policy level", "must not be empty")
	}
	e.policy = record
	return nil
}

// Complete ends a running attempt with an answer.
func (e *Execution) Complete(result Result, usage Usage, tracePath string, endedAt time.Time) error {
	return e.end(ExecutionCompleted, result, usage, "", tracePath, endedAt)
}

// Fail ends a running attempt with a reason; a partial result may be kept.
func (e *Execution) Fail(reason string, result Result, usage Usage, tracePath string, endedAt time.Time) error {
	if reason == "" {
		return invalid("failure reason", "must not be empty")
	}
	return e.end(ExecutionFailed, result, usage, reason, tracePath, endedAt)
}

// Cancel ends a running attempt because the operator stopped it.
func (e *Execution) Cancel(reason string, endedAt time.Time) error {
	if reason == "" {
		return invalid("cancel reason", "must not be empty")
	}
	return e.end(ExecutionCancelled, Result{}, Usage{}, reason, e.tracePath, endedAt)
}

// RequireDecision ends a running attempt on a tool call a person must decide.
func (e *Execution) RequireDecision(result Result, usage Usage, tracePath string, endedAt time.Time) error {
	if result.PendingDecision == nil {
		return invalid("pending decision", "is required when a decision is required")
	}
	return e.end(ExecutionDecisionRequired, result, usage, "", tracePath, endedAt)
}

func (e *Execution) end(outcome ExecutionOutcome, result Result, usage Usage, reason, tracePath string, endedAt time.Time) error {
	if e.outcome != ExecutionRunning {
		return StateError{Subject: "execution " + e.id.String(), Current: e.outcome.String(), Attempted: "end as " + outcome.String()}
	}
	if endedAt.Before(e.startedAt) {
		return invalid("ended at", "must not be before started at")
	}
	if err := usage.Validate(); err != nil {
		return err
	}
	e.outcome = outcome
	e.result = result
	e.usage = usage
	e.failureReason = reason
	e.tracePath = tracePath
	e.endedAt = endedAt
	return nil
}

// Snapshot returns the execution as plain data.
func (e Execution) Snapshot() ExecutionSnapshot {
	s := ExecutionSnapshot{
		ID:            e.id.String(),
		TaskID:        e.taskID.String(),
		BotID:         e.botID.String(),
		StartedAt:     e.startedAt,
		Outcome:       e.outcome.String(),
		Result:        e.result,
		Usage:         e.usage,
		FailureReason: e.failureReason,
		TracePath:     e.tracePath,
	}
	if e.policy.Level != "" {
		policy := e.policy
		s.Policy = &policy
	}
	if e.correlation.TraceID != "" {
		correlation := e.correlation
		s.Correlation = &correlation
	}
	if e.kind != ExecutionRun {
		s.Kind = e.kind.String()
		s.Decision = e.decision
	}
	s.NodeID = e.nodeID.String()
	if !e.leaseUntil.IsZero() {
		lease := e.leaseUntil
		s.LeaseUntil = &lease
	}
	if !e.endedAt.IsZero() {
		ended := e.endedAt
		s.EndedAt = &ended
	}
	return s
}

// RestoreExecution rebuilds an execution from a snapshot.
func RestoreExecution(s ExecutionSnapshot) (Execution, error) {
	id, err := NewExecutionID(s.ID)
	if err != nil {
		return Execution{}, err
	}
	task, err := NewTaskID(s.TaskID)
	if err != nil {
		return Execution{}, err
	}
	bot, err := NewBotID(s.BotID)
	if err != nil {
		return Execution{}, err
	}
	outcome, err := ParseExecutionOutcome(s.Outcome)
	if err != nil {
		return Execution{}, err
	}
	execution, err := NewExecution(id, task, bot, s.StartedAt)
	if err != nil {
		return Execution{}, err
	}
	if s.Policy != nil {
		if err := execution.RecordPolicy(*s.Policy); err != nil {
			return Execution{}, err
		}
	}
	if s.Correlation != nil {
		if err := execution.Correlate(CorrelationID(s.Correlation.TraceID)); err != nil {
			return Execution{}, err
		}
		if err := execution.RecordCorrelation(s.Correlation.Carried, s.Correlation.Path); err != nil {
			return Execution{}, err
		}
	}
	kind, err := ParseExecutionKind(s.Kind)
	if err != nil {
		return Execution{}, err
	}
	execution.kind = kind
	if kind == ExecutionDecide {
		if err := execution.Decide(s.Decision); err != nil {
			return Execution{}, err
		}
	}
	if s.NodeID != "" {
		node, err := NewNodeID(s.NodeID)
		if err != nil {
			return Execution{}, err
		}
		execution.nodeID = node
	}
	if s.LeaseUntil != nil {
		if execution.nodeID == "" {
			return Execution{}, invalid("lease until", "needs a node")
		}
		execution.leaseUntil = *s.LeaseUntil
	}
	if !outcome.IsFinal() {
		return execution, nil
	}
	if s.EndedAt == nil {
		return Execution{}, invalid("ended at", "is required by a finished execution")
	}
	switch outcome {
	case ExecutionCompleted:
		err = execution.Complete(s.Result, s.Usage, s.TracePath, *s.EndedAt)
	case ExecutionFailed:
		err = execution.Fail(s.FailureReason, s.Result, s.Usage, s.TracePath, *s.EndedAt)
	case ExecutionCancelled:
		execution.tracePath = s.TracePath
		err = execution.Cancel(s.FailureReason, *s.EndedAt)
	default:
		err = execution.RequireDecision(s.Result, s.Usage, s.TracePath, *s.EndedAt)
	}
	if err != nil {
		return Execution{}, err
	}
	return execution, nil
}
