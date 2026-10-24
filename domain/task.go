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

// FailureReason says why the task failed, when it did.
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

// Fail moves a Running task to Failed with a reason.
func (t *Task) Fail(reason string, now time.Time) error {
	if t.status != TaskRunning {
		return t.refuse("fail")
	}
	if reason == "" {
		return invalid("failure reason", "must not be empty")
	}
	t.status = TaskFailed
	t.failureReason = reason
	t.updatedAt = now
	return nil
}

// AwaitApproval parks a Running task until a person decides a tool call.
func (t *Task) AwaitApproval(now time.Time) error {
	if t.status != TaskRunning {
		return t.refuse("wait for approval")
	}
	t.status = TaskWaitingApproval
	t.updatedAt = now
	return nil
}

// Resume returns a WaitingApproval task to Running under a new execution,
// the one that carries the decision on the pending call.
func (t *Task) Resume(execution ExecutionID, now time.Time) error {
	if t.status != TaskWaitingApproval {
		return t.refuse("resume")
	}
	if execution == "" {
		return invalid("execution id", "must not be empty")
	}
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
	return TaskSnapshot{
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
		CreatedAt:      t.createdAt,
		UpdatedAt:      t.updatedAt,
	}
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
	if status == TaskFailed && s.FailureReason == "" {
		return Task{}, invalid("failure reason", "is required by a Failed task")
	}
	task.status = status
	task.failureReason = s.FailureReason
	task.updatedAt = s.UpdatedAt
	return task, nil
}
