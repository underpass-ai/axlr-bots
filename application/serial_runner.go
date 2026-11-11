package application

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// SerialRunner runs executions one at a time: the daemon's worker and
// its API share one, so two goroutines never start the same task, and
// the host carries one axlr-tui at a time. It also knows which task is
// running in process right now, so a cancel can interrupt it without
// waiting for the lock the run holds.
//
// Two locks, always taken in this order: slot, then mu (flightMu is
// taken last, alone). slot is held for the whole of Run, RunNext,
// Decide and ExpireApprovals: one in-process run at a time. mu guards
// the state changes (start, assign, settle, report, reconcile) and every
// method takes it, but the four that reach the runtime let go of it
// while the runtime runs (serial_runner_slot.go), so a node's poll or
// report waits for a state change, never for a 15-minute run.
// docs/architecture/concurrency.md says what each lock keeps out.
type SerialRunner struct {
	deps Dependencies
	// slotDeps are deps with the runtime and the decisions wrapped to
	// let go of mu while they run; only the four slot methods use them.
	slotDeps Dependencies
	slot     sync.Mutex
	mu       sync.Mutex
	flightMu sync.Mutex
	inFlight map[domain.TaskID]context.CancelFunc
}

// NewSerialRunner wires the runner.
func NewSerialRunner(deps Dependencies) *SerialRunner {
	r := &SerialRunner{deps: deps, inFlight: map[domain.TaskID]context.CancelFunc{}}
	r.slotDeps = deps
	if deps.Runtime != nil {
		r.slotDeps.Runtime = unlockedRuntime{inner: deps.Runtime, mu: &r.mu}
	}
	if deps.Decisions != nil {
		r.slotDeps.Decisions = unlockedDecisions{inner: deps.Decisions, mu: &r.mu}
	}
	return r
}

// lockSlot takes the slot, then the state lock, and returns what lets
// go of both.
func (r *SerialRunner) lockSlot() func() {
	r.slot.Lock()
	r.mu.Lock()
	return func() {
		r.mu.Unlock()
		r.slot.Unlock()
	}
}

// Run runs one Pending task, as RunTask does.
func (r *SerialRunner) Run(ctx context.Context, id domain.TaskID) (RunTaskResult, error) {
	defer r.lockSlot()()
	runCtx, done := r.track(ctx, id)
	defer done()
	return NewRunTask(r.slotDeps).Execute(runCtx, id)
}

// Decide decides a WaitingApproval task's pending call, as ApproveTask
// does; callID, when given, must name it. It holds the slot even for a
// decision a node carries: it cannot know which until it has read the
// task, and that read must come after the slot to keep the lock order.
func (r *SerialRunner) Decide(ctx context.Context, id domain.TaskID, decision, callID, actor string) (ApproveTaskResult, error) {
	defer r.lockSlot()()
	runCtx, done := r.track(ctx, id)
	defer done()
	return NewApproveTask(r.slotDeps).Execute(runCtx, id, decision, callID, actor)
}

// ExpireApprovals carries out every deadline that has passed, as
// ExpireApprovals does, under the same slot as the decisions: a call is
// never both decided by a person and denied by its deadline. It returns
// what it did, and the first error after trying every task.
func (r *SerialRunner) ExpireApprovals(ctx context.Context) ([]ExpiredApproval, error) {
	defer r.lockSlot()()
	uc := NewExpireApprovals(r.slotDeps)
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
	defer r.lockSlot()()
	id, found, err := NewRunNextPending(r.slotDeps).Pick(ctx)
	if err != nil || !found {
		return RunTaskResult{}, false, err
	}
	runCtx, done := r.track(ctx, id)
	defer done()
	result, err := NewRunTask(r.slotDeps).Execute(runCtx, id)
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

// running reports whether the task is being run in process right now.
func (r *SerialRunner) running(id domain.TaskID) bool {
	r.flightMu.Lock()
	defer r.flightMu.Unlock()
	_, ok := r.inFlight[id]
	return ok
}

// Reconcile fails what a dead run left Running, as ReconcileTasks does,
// under the same lock as the state changes, and never a task this
// process is running right now, however small the grace: the run is
// alive and will settle it. The command `task reconcile` runs in
// another process and cannot see this lock: only a lock on the state
// directory itself can keep it from a run's settle.
func (r *SerialRunner) Reconcile(ctx context.Context, grace time.Duration) (ReconcileTasksResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	uc := NewReconcileTasks(r.deps)
	uc.InFlight = r.running
	return uc.Execute(ctx, grace)
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

// Ask creates the task an execution asks another team for, as AskTeam
// does, under the same lock as the assignments and the reports: two
// questions to one team are picked one after the other and never land on
// one bot's single free slot, and the asking task is never changed by two
// of them at once.
func (r *SerialRunner) Ask(ctx context.Context, node domain.NodeID, execution domain.ExecutionID, command AskTeamCommand) (QuestionAnswer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewAskTeam(r.deps).Execute(ctx, node, execution, command)
}

// WatchNext answers a team's Jev watcher the next question of its round,
// as WatchNext does, under the same lock as the assignments and the
// reports: what it reads is never half changed.
func (r *SerialRunner) WatchNext(ctx context.Context, node domain.NodeID, execution domain.ExecutionID, after domain.TaskID) (WatchedQuestion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewWatchNext(r.deps).Execute(ctx, node, execution, after)
}

// DecideWatch carries out a watcher's decision, as DecideWatch does,
// under the same lock: a question is never abandoned while a node is
// handed it or reports it.
func (r *SerialRunner) DecideWatch(ctx context.Context, node domain.NodeID, execution domain.ExecutionID, decision WatchDecision) (WatchDecided, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewDecideWatch(r.deps).Execute(ctx, node, execution, decision)
}
