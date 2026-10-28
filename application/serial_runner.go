package application

import (
	"context"
	"fmt"
	"sync"

	"github.com/underpass-ai/axlr-bots/domain"
)

// SerialRunner runs executions one at a time: the daemon's worker and
// its API share one, so two goroutines never start the same task, and
// the host carries one axlr-tui at a time. It also knows which task is
// running in process right now, so a cancel can interrupt it without
// waiting for the lock the run holds.
type SerialRunner struct {
	deps     Dependencies
	mu       sync.Mutex
	flightMu sync.Mutex
	inFlight map[domain.TaskID]context.CancelFunc
}

// NewSerialRunner wires the runner.
func NewSerialRunner(deps Dependencies) *SerialRunner {
	return &SerialRunner{deps: deps, inFlight: map[domain.TaskID]context.CancelFunc{}}
}

// Run runs one Pending task, as RunTask does.
func (r *SerialRunner) Run(ctx context.Context, id domain.TaskID) (RunTaskResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	runCtx, done := r.track(ctx, id)
	defer done()
	return NewRunTask(r.deps).Execute(runCtx, id)
}

// Decide decides a WaitingApproval task's pending call, as ApproveTask
// does; callID, when given, must name it.
func (r *SerialRunner) Decide(ctx context.Context, id domain.TaskID, decision, callID string) (ApproveTaskResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	runCtx, done := r.track(ctx, id)
	defer done()
	return NewApproveTask(r.deps).Execute(runCtx, id, decision, callID)
}

// ExpireApprovals carries out every deadline that has passed, as
// ExpireApprovals does, under the same lock as the decisions: a call is
// never both decided by a person and denied by its deadline. It returns
// what it did, and the first error after trying every task.
func (r *SerialRunner) ExpireApprovals(ctx context.Context) ([]ExpiredApproval, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	uc := NewExpireApprovals(r.deps)
	due, first := uc.Due(ctx)
	var done []ExpiredApproval
	for _, id := range due {
		runCtx, untrack := r.track(ctx, id)
		expired, err := uc.Expire(runCtx, id)
		untrack()
		if err != nil {
			if first == nil {
				first = fmt.Errorf("expire approval of %s: %w", id, err)
			}
			continue
		}
		if expired.Action != "" {
			done = append(done, expired)
		}
	}
	return done, first
}

// RunNext runs the oldest Pending task, as RunNextPending does.
func (r *SerialRunner) RunNext(ctx context.Context) (RunTaskResult, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, found, err := NewRunNextPending(r.deps).Pick(ctx)
	if err != nil || !found {
		return RunTaskResult{}, false, err
	}
	runCtx, done := r.track(ctx, id)
	defer done()
	result, err := NewRunTask(r.deps).Execute(runCtx, id)
	if err != nil {
		return RunTaskResult{}, true, err
	}
	return result, true, nil
}

// Interrupt cancels the in-process run of the task, if one is going on,
// and reports whether it was. The run then ends as interrupted and its
// report is dropped, since CancelTask ended the execution first.
func (r *SerialRunner) Interrupt(id domain.TaskID) bool {
	r.flightMu.Lock()
	defer r.flightMu.Unlock()
	cancel, ok := r.inFlight[id]
	if ok {
		cancel()
	}
	return ok
}

// track derives the run's context and remembers how to cancel it while
// the run lasts.
func (r *SerialRunner) track(ctx context.Context, id domain.TaskID) (context.Context, func()) {
	runCtx, cancel := context.WithCancel(ctx)
	r.flightMu.Lock()
	r.inFlight[id] = cancel
	r.flightMu.Unlock()
	return runCtx, func() {
		r.flightMu.Lock()
		delete(r.inFlight, id)
		r.flightMu.Unlock()
		cancel()
	}
}

// NextAssignment hands a node its next execution, as NextAssignment
// does, under the same lock as the runs: a task is never both assigned
// to a node and run in process.
func (r *SerialRunner) NextAssignment(ctx context.Context, node domain.NodeID) (Assignment, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewNextAssignment(r.deps).Execute(ctx, node)
}

// Report settles a node's report, as ReportExecution does.
func (r *SerialRunner) Report(ctx context.Context, node domain.NodeID, execution domain.ExecutionID, report ExecutionReport) (ReportResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewReportExecution(r.deps).Execute(ctx, node, execution, report)
}

// RevokeNode revokes a node under the same lock as the assignments, so
// an execution is never handed to a node as it is being revoked.
func (r *SerialRunner) RevokeNode(ctx context.Context, node domain.NodeID) (RevokeNodeResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewRevokeNode(r.deps).Execute(ctx, node)
}

// Emit delivers an execution's event to its teammates, as EmitEvent
// does, under the same lock as the assignments and the reports.
func (r *SerialRunner) Emit(ctx context.Context, node domain.NodeID, execution domain.ExecutionID, command EmitEventCommand) (EmitAnswer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewEmitEvent(r.deps).Execute(ctx, node, execution, command)
}

// Publish delivers the operator's event on a team, as
// PublishGroupEvent does, under the same lock as the assignments.
func (r *SerialRunner) Publish(ctx context.Context, command PublishGroupEventCommand) (PublishAnswer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewPublishGroupEvent(r.deps).Execute(ctx, command)
}

// Subscribe adds the subscription an execution's bot asks for, as
// AskSubscription does, under the same lock as the assignments.
func (r *SerialRunner) Subscribe(ctx context.Context, node domain.NodeID, execution domain.ExecutionID, command SubscribeCommand) (SubscribeAnswer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewAskSubscription(r.deps).Execute(ctx, node, execution, command)
}

// Delegate creates the task an execution asks another bot for, as
// DelegateTask does, under the same lock as the assignments and the
// reports: the parent task is never changed by two of them at once.
func (r *SerialRunner) Delegate(ctx context.Context, node domain.NodeID, execution domain.ExecutionID, command DelegateCommand) (DelegationAnswer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewDelegateTask(r.deps).Execute(ctx, node, execution, command)
}
