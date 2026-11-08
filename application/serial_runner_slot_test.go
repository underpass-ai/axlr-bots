package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// heldRuntime holds every run until released (or until its context
// ends), so a test can act while a run is in the runtime.
type heldRuntime struct {
	started chan domain.TaskID
	release chan struct{}
}

func newHeldRuntime() *heldRuntime {
	return &heldRuntime{started: make(chan domain.TaskID, 8), release: make(chan struct{})}
}

func (b *heldRuntime) Execute(ctx context.Context, request ExecutionRequest) (ExecutionReport, error) {
	b.started <- request.TaskID
	select {
	case <-b.release:
		return ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "ran in process"}}, nil
	case <-ctx.Done():
		return ExecutionReport{}, ctx.Err()
	}
}

// within fails the test when call does not return within a second.
func within(t *testing.T, what string, call func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		call()
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("%s waited for the run in the runtime", what)
	}
}

// runInBackground starts runner.Run on the task and waits until the run
// is in the runtime; the returned channel gives its result.
type runOutcome struct {
	result RunTaskResult
	err    error
}

func runInBackground(t *testing.T, runner *SerialRunner, runtime *heldRuntime, id domain.TaskID) <-chan runOutcome {
	t.Helper()
	out := make(chan runOutcome, 1)
	go func() {
		result, err := runner.Run(context.Background(), id)
		out <- runOutcome{result, err}
	}()
	select {
	case started := <-runtime.started:
		if started != id {
			t.Fatalf("the runtime ran %s, want %s", started, id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run never reached the runtime")
	}
	return out
}

func TestSerialRunnerServesTheNodesWhileARunIsInTheRuntime(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	// A task waiting for a person, set up before the runtime blocks.
	waiting := submit(t, h, bot, "k-waiting").Task
	h.runtime.report = pendingReport("sess-1")
	if res, err := NewRunTask(h.deps).Execute(ctx, waiting.ID()); err != nil || res.Task.Status() != domain.TaskWaitingApproval {
		t.Fatalf("waiting task: %v %v", err, res.Task.Status())
	}
	local := submit(t, h, bot, "k-local").Task
	h.clock.now = t0.Add(time.Minute)
	remote := submit(t, h, bot, "k-remote").Task
	node := onlineNode(t, h, "web-1")

	runtime := newHeldRuntime()
	h.deps.Runtime = runtime
	runner := NewSerialRunner(h.deps)
	ran := runInBackground(t, runner, runtime, local.ID())

	// The node polls and reports while the run is in the runtime.
	var assignment Assignment
	within(t, "NextAssignment", func() {
		var found bool
		var err error
		assignment, found, err = runner.NextAssignment(ctx, node.ID())
		if err != nil || !found || assignment.Run == nil || assignment.Run.TaskID != remote.ID() {
			t.Errorf("assignment: %v %v %+v", err, found, assignment)
		}
	})
	within(t, "Report", func() {
		result, err := runner.Report(ctx, node.ID(), assignment.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "ran on the node"}})
		if err != nil || !result.Accepted || result.Task.ID() != remote.ID() || result.Task.Status() != domain.TaskCompleted {
			t.Errorf("report: %v %+v", err, result)
		}
	})
	// A person's decision waits for the slot: the host runs one
	// axlr-tui at a time, and Decide may be one.
	decided := make(chan error, 1)
	go func() {
		_, err := runner.Decide(ctx, waiting.ID(), DecisionApprove, "", "")
		decided <- err
	}()
	select {
	case err := <-decided:
		t.Fatalf("the decision did not wait for the run: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(runtime.release)
	outcome := <-ran
	if outcome.err != nil || outcome.result.Task.Status() != domain.TaskCompleted || outcome.result.Execution.Outcome() != domain.ExecutionCompleted {
		t.Errorf("run: %v %+v", outcome.err, outcome.result.Task.Snapshot())
	}
	if err := <-decided; err != nil {
		t.Errorf("decision: %v", err)
	}
	// Both settled as they said, and neither undid the other.
	for id, want := range map[domain.TaskID]string{local.ID(): "ran in process", remote.ID(): "ran on the node"} {
		task, _ := h.tasks.Get(ctx, id)
		execution, _ := h.execs.Get(ctx, task.CurrentExecutionID())
		if task.Status() != domain.TaskCompleted || execution.Outcome() != domain.ExecutionCompleted || h.store.reports[id] != want {
			t.Errorf("%s: %s %s %q", id, task.Status(), execution.Outcome(), h.store.reports[id])
		}
	}
	if task, _ := h.tasks.Get(ctx, waiting.ID()); task.Status() != domain.TaskCompleted {
		t.Errorf("decided task is %s", task.Status())
	}
}

func TestSerialRunnerReconcileLeavesAnInProcessRunAlone(t *testing.T) {
	for _, grace := range []time.Duration{0, time.Nanosecond} {
		h := newHarness()
		ctx := context.Background()
		bot := enabledBot(t, h)
		task := submit(t, h, bot, "k").Task
		runtime := newHeldRuntime()
		h.deps.Runtime = runtime
		runner := NewSerialRunner(h.deps)
		ran := runInBackground(t, runner, runtime, task.ID())

		// Long past the bot's timeout: only the run being alive saves it.
		h.clock.now = t0.Add(time.Hour)
		var result ReconcileTasksResult
		within(t, "Reconcile", func() {
			var err error
			result, err = runner.Reconcile(ctx, grace)
			if err != nil {
				t.Errorf("reconcile: %v", err)
			}
		})
		if len(result.Failed) != 0 || len(result.Skipped) != 1 || result.Skipped[0] != task.ID() || len(result.Orphaned) != 0 {
			t.Errorf("grace %s: reconcile = %+v", grace, result)
		}
		close(runtime.release)
		if outcome := <-ran; outcome.err != nil || outcome.result.Task.Status() != domain.TaskCompleted {
			t.Errorf("grace %s: run: %v %s", grace, outcome.err, outcome.result.Task.Status())
		}
		// Once the run has ended, a lost task is reconciled as before.
		left, _ := leftRunning(t, h, bot, "k-lost", t0)
		result, err := runner.Reconcile(ctx, grace)
		if err != nil || len(result.Failed) != 1 || result.Failed[0] != left.ID() {
			t.Errorf("grace %s: lost task: %v %+v", grace, err, result)
		}
	}
}

func TestPickAndANodeRaceForTheSamePendingTask(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	task := submit(t, h, bot, "k").Task
	runner := NewSerialRunner(h.deps)
	// The worker picks the oldest Pending task while no node may run it.
	id, found, err := NewRunNextPending(h.deps).Pick(ctx)
	if err != nil || !found || id != task.ID() {
		t.Fatalf("pick: %v %v %s", err, found, id)
	}
	// A node comes online and takes it first.
	node := onlineNode(t, h, "web-1")
	assignment, found, err := runner.NextAssignment(ctx, node.ID())
	if err != nil || !found || assignment.Run.TaskID != task.ID() {
		t.Fatalf("assignment: %v %v", err, found)
	}
	// The worker's start loses: a StateError, audited, and no second
	// execution.
	_, err = runner.Run(ctx, id)
	var state domain.StateError
	if !errors.As(err, &state) {
		t.Fatalf("the losing start: %v", err)
	}
	executions, _ := h.execs.List(ctx)
	if len(executions) != 1 || len(h.runtime.requests) != 0 {
		t.Errorf("%d executions, %d in-process runs", len(executions), len(h.runtime.requests))
	}
	if last := h.log.audits[len(h.log.audits)-1]; last.Action != "task.run" || last.Outcome != domain.AuditDenied {
		t.Errorf("last audit = %+v", last)
	}
}
