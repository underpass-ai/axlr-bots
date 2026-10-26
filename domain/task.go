package domain

import (
	"encoding/json"
	"time"
)

// Task is one piece of work asked of a bot: an objective, a status, the
// trigger it came through with the event that fired it, and the
// executions that tried it. Submissions are idempotent per bot and key;
// the correlation id is the trace id every execution and event of the
// task carries.
type Task struct {
	id             TaskID
	botID          BotID
	objective      Objective
	status         TaskStatus
	idempotencyKey IdempotencyKey
	correlationID  CorrelationID
	trigger        TriggerRef
	input          json.RawMessage
	executionIDs   []ExecutionID
	failureReason  string
	approval       *ApprovalWait
	lineage        *TaskLineage
	children       []TaskID
	grantedModel   int
	grantedTools   int
	createdAt      time.Time
	updatedAt      time.Time
}

// NewTask creates a Pending task.
func NewTask(id TaskID, bot BotID, objective Objective, key IdempotencyKey, correlation CorrelationID, now time.Time) (Task, error) {
	if now.IsZero() {
		return Task{}, invalid("created at", "must not be zero")
	}
	return Task{
		id: id, botID: bot, objective: objective, status: TaskPending,
		idempotencyKey: key, correlationID: correlation, trigger: ManualTrigger(),
		createdAt: now, updatedAt: now,
	}, nil
}

// Attribute records the trigger the task came through and the event
// that fired it (a webhook's body, a timer's slot), before the task is
// first saved.
func (t *Task) Attribute(trigger TriggerRef, input []byte) error {
	ref, err := NewTriggerRef(trigger.Type.String(), trigger.Name)
	if err != nil {
		return err
	}
	parsed, err := NewTaskInput(input)
	if err != nil {
		return err
	}
	t.trigger, t.input = ref, parsed
	return nil
}

// Link records where a delegated task came from, before the task is
// first saved.
func (t *Task) Link(lineage TaskLineage) error {
	if err := lineage.Validate(); err != nil {
		return err
	}
	lineage.Chain = append([]string(nil), lineage.Chain...)
	t.lineage = &lineage
	return nil
}

// Lineage is where a delegated task came from; false for a task nobody
// delegated.
func (t Task) Lineage() (TaskLineage, bool) {
	if t.lineage == nil {
		return TaskLineage{}, false
	}
	copied := *t.lineage
	copied.Chain = append([]string(nil), t.lineage.Chain...)
	return copied, true
}

// Depth is how many delegations led to the task: 0 for one nobody
// delegated.
func (t Task) Depth() int {
	if t.lineage == nil {
		return 0
	}
	return t.lineage.Depth
}

// Children lists the tasks this task delegated, oldest first.
func (t Task) Children() []TaskID { return append([]TaskID(nil), t.children...) }

// Granted is what the task has handed its children so far: model calls
// and tool calls out of its own limits.
func (t Task) Granted() (modelCalls, toolCalls int) { return t.grantedModel, t.grantedTools }

// AdoptChild records a task this Running task delegated and what it was
// granted out of the task's own limits.
func (t *Task) AdoptChild(child TaskID, grant DelegationGrant, now time.Time) error {
	if t.status != TaskRunning {
		return t.refuse("delegate")
	}
	if child == "" || child == t.id {
		return invalid("child task", "must be another task")
	}
	if err := grant.Validate(); err != nil {
		return err
	}
	t.children = append(t.children, child)
	t.grantedModel += grant.MaxModelCalls
	t.grantedTools += grant.MaxToolCalls
	t.updatedAt = now
	return nil
}

// DeadlinePassed reports a delegated task whose grant's deadline has
// come.
func (t Task) DeadlinePassed(now time.Time) bool {
	return t.lineage != nil && !now.Before(t.lineage.Grant.Deadline)
}

// MissDeadline fails a delegated task that has not finished when its
// deadline came: Pending (it never ran), WaitingApproval (nobody decided
// in time) or Running (its node never reported). A task that waited, or
// whose last wait had already expired, ends Expired.
func (t *Task) MissDeadline(now time.Time) error {
	if t.lineage == nil || t.status.IsTerminal() || !t.DeadlinePassed(now) {
		return t.refuse("miss its deadline")
	}
	t.closeApproval(ApprovalExpired, now)
	t.status = t.failedStatus()
	t.failureReason = "deadline: the delegation's deadline " + t.lineage.Grant.Deadline.UTC().Format(time.RFC3339) + " passed before the task ended"
	t.updatedAt = now
	return nil
}

// Trigger is the trigger the task came through.
func (t Task) Trigger() TriggerRef { return t.trigger }

// Input is the event that fired the task, as JSON; nil for a manual task.
func (t Task) Input() json.RawMessage { return append(json.RawMessage(nil), t.input...) }

// ID is the task's identifier.
func (t Task) ID() TaskID { return t.id }

// BotID is the bot the task belongs to.
func (t Task) BotID() BotID { return t.botID }

// Objective is what the task asks for.
func (t Task) Objective() Objective { return t.objective }

// Status is the task's life-cycle state.
func (t Task) Status() TaskStatus { return t.status }

// IdempotencyKey is the key the submission carried.
func (t Task) IdempotencyKey() IdempotencyKey { return t.idempotencyKey }

// CorrelationID ties the task's events to upstream work.
func (t Task) CorrelationID() CorrelationID { return t.correlationID }

// ExecutionIDs lists every execution that tried the task, oldest first.
func (t Task) ExecutionIDs() []ExecutionID {
	out := make([]ExecutionID, len(t.executionIDs))
	copy(out, t.executionIDs)
	return out
}

// CurrentExecutionID is the latest execution, or empty when none ran.
func (t Task) CurrentExecutionID() ExecutionID {
	if len(t.executionIDs) == 0 {
		return ""
	}
	return t.executionIDs[len(t.executionIDs)-1]
}

// FailureReason says why the task failed or expired, when it did.
func (t Task) FailureReason() string { return t.failureReason }

// CreatedAt is when the task was submitted.
func (t Task) CreatedAt() time.Time { return t.createdAt }

// UpdatedAt is when the task last changed.
func (t Task) UpdatedAt() time.Time { return t.updatedAt }

// Start moves a Pending task to Running under the given execution.
func (t *Task) Start(execution ExecutionID, now time.Time) error {
	if t.status != TaskPending {
		return t.refuse("start")
	}
	if execution == "" {
		return invalid("execution id", "must not be empty")
	}
	t.executionIDs = append(t.executionIDs, execution)
	t.status = TaskRunning
	t.updatedAt = now
	return nil
}

// Complete moves a Running task to Completed.
func (t *Task) Complete(now time.Time) error {
	if t.status != TaskRunning {
		return t.refuse("complete")
	}
	t.status = TaskCompleted
	t.updatedAt = now
	return nil
}

// Fail moves a Running task to Failed with a reason. A task whose last
// approval expired ends Expired instead: it went on with the denial of
// the call nobody decided in time, the call never ran, and the reason
// says what ended the run.
func (t *Task) Fail(reason string, now time.Time) error {
	if t.status != TaskRunning {
		return t.refuse("fail")
	}
	if reason == "" {
		return invalid("failure reason", "must not be empty")
	}
	t.status = t.failedStatus()
	t.failureReason = reason
	t.updatedAt = now
	return nil
}

// failedStatus is how a task that did not complete ends: Expired when
// its last wait for a person expired, Failed otherwise.
func (t Task) failedStatus() TaskStatus {
	if t.approval != nil && t.approval.Outcome == ApprovalExpired {
		return TaskExpired
	}
	return TaskFailed
}

// AwaitApproval parks a Running task until a person decides a tool
// call: the wait names the call and the deadline it must be decided by.
func (t *Task) AwaitApproval(wait ApprovalWait, now time.Time) error {
	if t.status != TaskRunning {
		return t.refuse("wait for approval")
	}
	wait.Outcome, wait.ClosedAt = "", nil
	if err := wait.Validate(); err != nil {
		return err
	}
	t.approval = &wait
	t.status = TaskWaitingApproval
	t.updatedAt = now
	return nil
}

// RecordApproval gives a WaitingApproval task that waited before
// approvals had deadlines the wait it is under; a task that has one
// keeps it.
func (t *Task) RecordApproval(wait ApprovalWait) error {
	if t.status != TaskWaitingApproval {
		return t.refuse("record an approval wait")
	}
	if t.approval != nil && t.approval.Open() {
		return t.refuse("record a second approval wait")
	}
	wait.Outcome, wait.ClosedAt = "", nil
	if err := wait.Validate(); err != nil {
		return err
	}
	t.approval = &wait
	return nil
}

// Approval is the last wait for a person's decision, open or closed,
// and false when the task never waited under a deadline.
func (t Task) Approval() (ApprovalWait, bool) {
	if t.approval == nil {
		return ApprovalWait{}, false
	}
	return *t.approval, true
}

// ApprovalExpired reports a WaitingApproval task whose deadline has passed.
func (t Task) ApprovalExpired(now time.Time) bool {
	return t.status == TaskWaitingApproval && t.approval != nil && t.approval.Open() && now.After(t.approval.Deadline)
}

// ExpireApproval closes the open wait as expired; the task stays
// WaitingApproval until it is cancelled or resumed with the denial.
func (t *Task) ExpireApproval(now time.Time) error {
	if t.status != TaskWaitingApproval || t.approval == nil || !t.approval.Open() {
		return t.refuse("expire its approval")
	}
	t.closeApproval(ApprovalExpired, now)
	return nil
}

// EndExpired ends a WaitingApproval task whose wait expired without
// handing the session a denial (onExpire cancel, or a paused bot that
// cannot carry one): the task is Expired, the deadline its reason.
func (t *Task) EndExpired(now time.Time) error {
	if t.status != TaskWaitingApproval || t.approval == nil || t.approval.Outcome != ApprovalExpired {
		return t.refuse("end expired")
	}
	call := ""
	if t.approval.CallID != "" {
		call = " " + t.approval.CallID
		if t.approval.Tool != "" {
			call += " (" + t.approval.Tool + ")"
		}
	}
	t.status = TaskExpired
	t.failureReason = "approval expired: nobody decided the call" + call + " by " + t.approval.Deadline.UTC().Format(time.RFC3339)
	t.updatedAt = now
	return nil
}

// closeApproval ends the open wait with the outcome, if there is one.
func (t *Task) closeApproval(outcome string, now time.Time) {
	if t.approval == nil || !t.approval.Open() {
		return
	}
	// A copy, not the shared wait: a Task is a value, and a copy of it
	// must not see this one close its wait.
	closed, at := *t.approval, now
	closed.Outcome, closed.ClosedAt = outcome, &at
	t.approval = &closed
}

// Resume returns a WaitingApproval task to Running under a new execution,
// the one that carries the decision on the pending call; the wait is
// closed with the outcome (approved, denied, or expired when the
// deadline denied it).
func (t *Task) Resume(execution ExecutionID, outcome string, now time.Time) error {
	if t.status != TaskWaitingApproval {
		return t.refuse("resume")
	}
	if execution == "" {
		return invalid("execution id", "must not be empty")
	}
	switch outcome {
	case ApprovalApproved, ApprovalDenied, ApprovalExpired:
	default:
		return invalid("approval outcome", "a resumed task is approved, denied or expired")
	}
	t.closeApproval(outcome, now)
	t.executionIDs = append(t.executionIDs, execution)
	t.status = TaskRunning
	t.updatedAt = now
	return nil
}

// Cancel stops a task that has not finished.
func (t *Task) Cancel(now time.Time) error {
	if t.status.IsTerminal() {
		return t.refuse("cancel")
	}
	t.closeApproval(ApprovalCancelled, now)
	t.status = TaskCancelled
	t.updatedAt = now
	return nil
}

func (t Task) refuse(attempted string) error {
	return StateError{Subject: "task " + t.id.String(), Current: t.status.String(), Attempted: attempted}
}

// Snapshot returns the task as plain data.
func (t Task) Snapshot() TaskSnapshot {
	ids := make([]string, len(t.executionIDs))
	for i, id := range t.executionIDs {
		ids[i] = id.String()
	}
	var approval *ApprovalWait
	if t.approval != nil {
		copied := *t.approval
		approval = &copied
	}
	var children []string
	for _, child := range t.children {
		children = append(children, child.String())
	}
	snapshot := TaskSnapshot{
		ID:             t.id.String(),
		BotID:          t.botID.String(),
		Objective:      t.objective.String(),
		Status:         t.status.String(),
		IdempotencyKey: t.idempotencyKey.String(),
		CorrelationID:  t.correlationID.String(),
		Trigger:        TriggerRefSnapshot{Type: t.trigger.Type.String(), Name: t.trigger.Name},
		Input:          t.Input(),
		ExecutionIDs:   ids,
		FailureReason:  t.failureReason,
		Approval:       approval,
		Children:       children,
		GrantedModel:   t.grantedModel,
		GrantedTools:   t.grantedTools,
		CreatedAt:      t.createdAt,
		UpdatedAt:      t.updatedAt,
	}
	if lineage, ok := t.Lineage(); ok {
		snapshot.Delegation = &lineage
	}
	return snapshot
}

// RestoreTask rebuilds a task from a snapshot, validating every value.
func RestoreTask(s TaskSnapshot) (Task, error) {
	id, err := NewTaskID(s.ID)
	if err != nil {
		return Task{}, err
	}
	bot, err := NewBotID(s.BotID)
	if err != nil {
		return Task{}, err
	}
	objective, err := NewObjective(s.Objective)
	if err != nil {
		return Task{}, err
	}
	key, err := NewIdempotencyKey(s.IdempotencyKey)
	if err != nil {
		return Task{}, err
	}
	correlation, err := NewCorrelationID(s.CorrelationID)
	if err != nil {
		return Task{}, err
	}
	status, err := ParseTaskStatus(s.Status)
	if err != nil {
		return Task{}, err
	}
	task, err := NewTask(id, bot, objective, key, correlation, s.CreatedAt)
	if err != nil {
		return Task{}, err
	}
	if s.UpdatedAt.IsZero() {
		return Task{}, invalid("updated at", "must not be zero")
	}
	trigger, err := NewTriggerRef(s.Trigger.Type, s.Trigger.Name)
	if err != nil {
		return Task{}, err
	}
	if err := task.Attribute(trigger, s.Input); err != nil {
		return Task{}, err
	}
	for _, raw := range s.ExecutionIDs {
		exec, err := NewExecutionID(raw)
		if err != nil {
			return Task{}, err
		}
		task.executionIDs = append(task.executionIDs, exec)
	}
	if (status == TaskFailed || status == TaskExpired) && s.FailureReason == "" {
		return Task{}, invalid("failure reason", "is required by a Failed or Expired task")
	}
	if status == TaskExpired && (s.Approval == nil || s.Approval.Outcome != ApprovalExpired) {
		return Task{}, invalid("approval", "an Expired task needs an expired wait")
	}
	if s.Approval != nil {
		if err := s.Approval.Validate(); err != nil {
			return Task{}, err
		}
		if s.Approval.Open() && status != TaskWaitingApproval {
			return Task{}, invalid("approval", "an open wait needs a WaitingApproval task")
		}
		approval := *s.Approval
		task.approval = &approval
	}
	if s.Delegation != nil {
		if err := task.Link(*s.Delegation); err != nil {
			return Task{}, err
		}
	}
	for _, raw := range s.Children {
		child, err := NewTaskID(raw)
		if err != nil {
			return Task{}, err
		}
		task.children = append(task.children, child)
	}
	if s.GrantedModel < 0 || s.GrantedTools < 0 {
		return Task{}, invalid("granted", "must not be negative")
	}
	task.grantedModel, task.grantedTools = s.GrantedModel, s.GrantedTools
	task.status = status
	task.failureReason = s.FailureReason
	task.updatedAt = s.UpdatedAt
	return task, nil
}
