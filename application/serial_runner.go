package application

import (
	"context"
	"sync"

	"github.com/underpass-ai/axlr-bots/domain"
)

// SerialRunner runs executions one at a time: the daemon's worker and
// its API share one, so two goroutines never start the same task, and
// the host carries one axlr-tui at a time.
type SerialRunner struct {
	deps Dependencies
	mu   sync.Mutex
}

// NewSerialRunner wires the runner.
func NewSerialRunner(deps Dependencies) *SerialRunner {
	return &SerialRunner{deps: deps}
}

// Run runs one Pending task, as RunTask does.
func (r *SerialRunner) Run(ctx context.Context, id domain.TaskID) (RunTaskResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewRunTask(r.deps).Execute(ctx, id)
}

// Decide decides a WaitingApproval task's pending call, as ApproveTask does.
func (r *SerialRunner) Decide(ctx context.Context, id domain.TaskID, decision string) (ApproveTaskResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewApproveTask(r.deps).Execute(ctx, id, decision)
}

// RunNext runs the oldest Pending task, as RunNextPending does.
func (r *SerialRunner) RunNext(ctx context.Context) (RunTaskResult, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewRunNextPending(r.deps).Execute(ctx)
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
