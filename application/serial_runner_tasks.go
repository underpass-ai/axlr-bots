package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// The operator's task writers go through the runner too,
// under mu, as the bot writers do (serial_runner_bots.go). Each reads a
// task, changes it and saves it, and so does a node's
// report or a run's settle under mu: outside the lock, a cancel that
// read the task Running while a report saved its execution Completed
// saved the task Cancelled over the report's Completed (QA L8, 5 November
// 2026). A submission and a fire may preempt the bot's work
// in progress (an urgent task), which is the same write. None reaches
// the runtime, so none takes the slot; the callers that also hold the
// submissions lock take it first.

// CancelTask cancels a task, as CancelTask does; a run in process is
// interrupted by the cancel event it publishes.
func (r *SerialRunner) CancelTask(ctx context.Context, id domain.TaskID) (CancelTaskResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewCancelTask(r.deps).Execute(ctx, id)
}

// SendTaskMessage records the operator's message for a running task, as
// SendTaskMessage does: it reads the task and its execution as no
// report is changing them.
func (r *SerialRunner) SendTaskMessage(ctx context.Context, id domain.TaskID, text string) (SentTaskMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewSendTaskMessage(r.deps).Execute(ctx, id, text)
}

// SubmitTask accepts a manual task, as SubmitTask does; an urgent one
// preempts the bot's work in progress under the lock.
func (r *SerialRunner) SubmitTask(ctx context.Context, command SubmitTaskCommand) (SubmitTaskResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewSubmitTask(r.deps).Execute(ctx, command)
}

// FireTrigger fires a bot's trigger, as FireTrigger does.
func (r *SerialRunner) FireTrigger(ctx context.Context, command FireTriggerCommand) (FireTriggerResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewFireTrigger(r.deps).Execute(ctx, command)
}
