package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// The operator's task and incident writers go through the runner too,
// under mu, as the bot writers do (serial_runner_bots.go). Each reads a
// task or an incident, changes it and saves it, and so does a node's
// report or a run's settle under mu: outside the lock, a cancel that
// read the task Running while a report saved its execution Completed
// saved the task Cancelled over the report's Completed (QA L8, 5 November
// 2026). A submission, a fire and an incident may preempt the bot's work
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

// ResolveIncident closes an incident, as ResolveIncident does, never at
// the moment a task's end moves it.
func (r *SerialRunner) ResolveIncident(ctx context.Context, id domain.IncidentID, reason string) (domain.Incident, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewResolveIncident(r.deps).Execute(ctx, id, reason)
}

// ApprovePostmortem approves an incident's postmortem, as
// ReviewPostmortem does, never at the moment a task's end moves it.
func (r *SerialRunner) ApprovePostmortem(ctx context.Context, id domain.IncidentID, actor string, read domain.TaskID) (domain.Incident, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewReviewPostmortem(r.deps).Approve(ctx, id, actor, read)
}

// SendBackPostmortem sends an incident's postmortem back with a note, as
// ReviewPostmortem does.
func (r *SerialRunner) SendBackPostmortem(ctx context.Context, id domain.IncidentID, actor, note string, read domain.TaskID) (domain.Incident, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewReviewPostmortem(r.deps).SendBack(ctx, id, actor, note, read)
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

// OpenIncident opens, repeats or reopens an incident, as OpenIncident
// does.
func (r *SerialRunner) OpenIncident(ctx context.Context, command OpenIncidentCommand) (OpenIncidentResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewOpenIncident(r.deps).Execute(ctx, command)
}

// LowerAutonomy takes a runbook down by a person's hand, as
// LowerAutonomy does, beside the signals of the same runbook.
func (r *SerialRunner) LowerAutonomy(ctx context.Context, id domain.AutonomyID, actor, reason string) (AutonomyView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewLowerAutonomy(r.deps).Execute(ctx, id, actor, reason)
}

// MarkDiagnosis records a person's verdict on an incident's diagnosis, as
// MarkDiagnosis does, beside a report's or a settle's signals of the same
// runbook.
func (r *SerialRunner) MarkDiagnosis(ctx context.Context, id domain.IncidentID, verdict, actor string) (AutonomyView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewMarkDiagnosis(r.deps).Execute(ctx, id, verdict, actor)
}
