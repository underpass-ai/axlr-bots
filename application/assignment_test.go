package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

func TestNextAssignmentStartsThePendingTaskOnTheNode(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	node := onlineNode(t, h, "web-1")
	if _, found, err := NewNextAssignment(h.deps).Execute(ctx, node.ID()); err != nil || found {
		t.Fatalf("nothing pending: %v %v", err, found)
	}
	h.clock.now = t0.Add(time.Minute)
	younger := submit(t, h, bot, "k-young").Task
	h.clock.now = t0
	older := submit(t, h, bot, "k-old").Task
	h.log.events = nil
	h.clock.now = t0.Add(2 * time.Minute)

	assignment, found, err := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	if err != nil || !found {
		t.Fatalf("assignment: %v %v", err, found)
	}
	if assignment.Kind != domain.ExecutionRun || assignment.Run == nil || assignment.Decide != nil || assignment.Run.TaskID != older.ID() || assignment.Run.Objective != older.Objective() || assignment.Run.Model != "anthropic/claude-haiku-5.5" || !assignment.LeaseUntil.Equal(h.clock.now.Add(LeaseDuration)) {
		t.Errorf("assignment = %+v run = %+v", assignment, assignment.Run)
	}
	task, _ := h.tasks.Get(ctx, older.ID())
	execution, _ := h.execs.Get(ctx, assignment.ExecutionID)
	if task.Status() != domain.TaskRunning || task.CurrentExecutionID() != assignment.ExecutionID || execution.NodeID() != "web-1" || !execution.LeaseUntil().Equal(assignment.LeaseUntil) || execution.Outcome() != domain.ExecutionRunning {
		t.Errorf("task = %+v execution = %+v", task.Snapshot(), execution.Snapshot())
	}
	if got := h.log.eventTypes(); len(got) != 2 || got[0] != "bot.task.started" || got[1] != "execution.assigned" || h.log.events[1].Payload["nodeId"] != "web-1" {
		t.Errorf("events = %v", got)
	}
	// The younger task goes next; then nothing; the runtime was never called.
	next, found, _ := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	if !found || next.Run.TaskID != younger.ID() {
		t.Errorf("next = %+v", next)
	}
	if _, found, _ := NewNextAssignment(h.deps).Execute(ctx, node.ID()); found {
		t.Error("a third assignment appeared")
	}
	if len(h.runtime.requests) != 0 {
		t.Error("the control plane ran the task itself")
	}
	// RunNextPending leaves a node-backed bot's tasks alone.
	submit(t, h, bot, "k-3")
	if _, ran, err := NewRunNextPending(h.deps).Execute(ctx); err != nil || ran {
		t.Errorf("in-process run of a node-backed task: %v %v", err, ran)
	}
}

func TestNextAssignmentHonoursPlacementAndStatus(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	d := sampleDefinition()
	d.Name = "placed"
	d.Placement = domain.PlacementSnapshot{Selector: map[string]string{"host": "web-1"}}
	bot, err := NewRegisterBot(h.deps).Execute(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	bot, _ = NewEnableBot(h.deps).Execute(ctx, bot.ID())
	submit(t, h, bot, "k")
	other := onlineNode(t, h, "db-1")
	if _, found, err := NewNextAssignment(h.deps).Execute(ctx, other.ID()); err != nil || found {
		t.Errorf("a node the placement excludes got the task: %v %v", err, found)
	}
	web := onlineNode(t, h, "web-1")
	if _, found, err := NewNextAssignment(h.deps).Execute(ctx, web.ID()); err != nil || !found {
		t.Errorf("the placed node got nothing: %v %v", err, found)
	}
	// A paused bot's tasks wait; an offline or unknown node is refused.
	submit(t, h, bot, "k2")
	paused, _ := h.bots.Get(ctx, bot.ID())
	_ = paused.Pause(t0)
	_ = h.bots.Save(ctx, paused)
	if _, found, err := NewNextAssignment(h.deps).Execute(ctx, web.ID()); err != nil || found {
		t.Errorf("paused bot's task assigned: %v %v", err, found)
	}
	h.clock.now = t0.Add(time.Hour)
	if _, err := NewMarkOfflineNodes(h.deps).Execute(ctx, 0); err != nil {
		t.Fatal(err)
	}
	var serr domain.StateError
	if _, _, err := NewNextAssignment(h.deps).Execute(ctx, web.ID()); !errors.As(err, &serr) {
		t.Errorf("offline node: %v", err)
	}
	if _, _, err := NewNextAssignment(h.deps).Execute(ctx, "ghost"); !domain.IsNotFound(err) {
		t.Errorf("unknown node: %v", err)
	}
}

func TestReportExecutionSettlesOnceAndRenewLeaseExtends(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	node := onlineNode(t, h, "web-1")
	task := submit(t, h, bot, "k").Task
	assignment, _, err := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	if err != nil {
		t.Fatal(err)
	}
	h.clock.now = t0.Add(time.Minute)
	until, err := NewRenewLease(h.deps).Execute(ctx, node.ID(), assignment.ExecutionID)
	if err != nil || !until.Equal(h.clock.now.Add(LeaseDuration)) {
		t.Errorf("renew: %v %s", err, until)
	}
	if _, err := NewRenewLease(h.deps).Execute(ctx, "db-1", assignment.ExecutionID); err == nil {
		t.Error("another node renewed the lease")
	}
	if _, err := NewRenewLease(h.deps).Execute(ctx, node.ID(), "exec-missing"); !domain.IsNotFound(err) {
		t.Errorf("unknown execution: %v", err)
	}
	h.log.events = nil
	report := ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "3 files", SessionID: "sess-1"}, Usage: domain.Usage{Requests: 1}, TracePath: "node:web-1:/traces/exec.jsonl", Policy: &domain.PolicyRecord{Level: "observe", Enforced: true}}
	if _, err := NewReportExecution(h.deps).Execute(ctx, "db-1", assignment.ExecutionID, report); err == nil {
		t.Error("another node reported the execution")
	}
	result, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), assignment.ExecutionID, report)
	if err != nil || !result.Accepted || result.Task.Status() != domain.TaskCompleted || result.Execution.Outcome() != domain.ExecutionCompleted || result.Execution.TracePath() != report.TracePath || !result.Execution.Policy().Enforced {
		t.Fatalf("report: %v %+v", err, result)
	}
	if h.store.reports[task.ID()] != "3 files" {
		t.Error("report artifact missing")
	}
	if got := h.log.eventTypes(); len(got) != 2 || got[0] != "bot.task.completed" || got[1] != "execution.reported" {
		t.Errorf("events = %v", got)
	}
	// The same report again is accepted and changes nothing.
	again, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), assignment.ExecutionID, ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "late"})
	if err != nil || again.Accepted || again.Execution.Outcome() != domain.ExecutionCompleted || len(h.log.events) != 2 {
		t.Errorf("second report: %v %+v", err, again)
	}
	if _, err := NewRenewLease(h.deps).Execute(ctx, node.ID(), assignment.ExecutionID); err == nil {
		t.Error("lease renewed on an ended execution")
	}
	if _, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), "exec-missing", report); !domain.IsNotFound(err) {
		t.Errorf("unknown execution: %v", err)
	}
	// A report that fails the run fails the task with the reason.
	task2 := submit(t, h, bot, "k2").Task
	a2, _, _ := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	failed, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), a2.ExecutionID, ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "axlr died"})
	if err != nil || failed.Task.ID() != task2.ID() || failed.Task.Status() != domain.TaskFailed || failed.Task.FailureReason() != "axlr died" {
		t.Errorf("failed report: %v %+v", err, failed.Task.Snapshot())
	}
}

func TestApproveTaskQueuesTheDecisionForTheNode(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	node := onlineNode(t, h, "web-1")
	task := submit(t, h, bot, "k").Task
	first, _, _ := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	pending := ExecutionReport{Outcome: domain.ExecutionDecisionRequired, Result: domain.Result{SessionID: "sess-1", PendingDecision: &domain.PendingDecision{Tool: "local_write", CallID: "c1", SessionID: "sess-1"}}}
	if _, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), first.ExecutionID, pending); err != nil {
		t.Fatal(err)
	}
	h.log.events = nil
	decided, err := NewApproveTask(h.deps).Execute(ctx, task.ID(), DecisionApprove, "")
	if err != nil {
		t.Fatal(err)
	}
	if decided.Task.Status() != domain.TaskRunning || decided.Execution.Kind() != domain.ExecutionDecide || decided.Execution.Decision() != "approve" || decided.Execution.NodeID() != "web-1" || !decided.Execution.LeaseUntil().IsZero() || decided.Execution.Outcome() != domain.ExecutionRunning {
		t.Errorf("decided = %+v", decided.Execution.Snapshot())
	}
	if len(h.decider.requests) != 0 {
		t.Error("the control plane decided in process")
	}
	if got := h.log.eventTypes(); len(got) != 1 || got[0] != "bot.task.decided" || h.log.events[0].Payload["nodeId"] != "web-1" {
		t.Errorf("events = %v", got)
	}
	// The node's next poll gets the decision, with the session and call.
	assignment, found, err := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	if err != nil || !found || assignment.Kind != domain.ExecutionDecide || assignment.Decide == nil || assignment.Run != nil {
		t.Fatalf("assignment: %v %v %+v", err, found, assignment)
	}
	if assignment.Decide.SessionID != "sess-1" || assignment.Decide.CallID != "c1" || assignment.Decide.Decision != "approve" || assignment.ExecutionID != decided.Execution.ID() || len(assignment.Decide.AllowedTools) != 2 {
		t.Errorf("decide = %+v", assignment.Decide)
	}
	if _, found, _ := NewNextAssignment(h.deps).Execute(ctx, node.ID()); found {
		t.Error("the decision was handed out twice")
	}
	result, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), assignment.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "written"}})
	if err != nil || result.Task.Status() != domain.TaskCompleted || len(result.Task.ExecutionIDs()) != 2 {
		t.Errorf("report of the decision: %v %+v", err, result.Task.Snapshot())
	}
	if last := h.log.audits[len(h.log.audits)-1]; last.Action != "task.decide" {
		t.Errorf("audit = %+v", last)
	}
}

// An execution queued for the node that cannot run (its task never
// took it: the process died between the two saves; or it decides a call
// that is not there) is failed as an orphan, and the node gets its next
// task instead of an error on every poll.
func TestNextAssignmentFailsAnOrphanAndHandsOutThePendingTask(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	node := onlineNode(t, h, "web-1")
	task := submit(t, h, bot, "k").Task
	// A decision queued for the node whose task is still Pending: it was
	// never saved with it.
	orphan, _ := domain.NewExecution("exec-odd", task.ID(), bot.ID(), t0)
	_ = orphan.Decide("approve")
	_ = orphan.AssignTo(node.ID())
	_ = h.execs.Save(ctx, orphan)
	// A decision its Running task does hold, with no earlier execution
	// holding a pending call.
	broken := submit(t, h, bot, "k-broken").Task
	decide, _ := domain.NewExecution("exec-odder", broken.ID(), bot.ID(), t0)
	_ = decide.Decide("approve")
	_ = decide.AssignTo(node.ID())
	_ = h.execs.Save(ctx, decide)
	_ = broken.Start("exec-odder", t0)
	_ = h.tasks.Save(ctx, broken)
	// One whose task is gone.
	lost, _ := domain.NewExecution("exec-gone", "task-gone", bot.ID(), t0)
	_ = lost.AssignTo(node.ID())
	_ = h.execs.Save(ctx, lost)
	assignment, found, err := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	if err != nil || !found || assignment.Run == nil || assignment.Run.TaskID != task.ID() {
		t.Fatalf("assignment = %v %v %+v", err, found, assignment)
	}
	for _, id := range []domain.ExecutionID{"exec-odd", "exec-odder", "exec-gone"} {
		failed, _ := h.execs.Get(ctx, id)
		if failed.Outcome() != domain.ExecutionFailed || !strings.HasPrefix(failed.FailureReason(), "orphan: ") {
			t.Errorf("%s = %s %q", id, failed.Outcome(), failed.FailureReason())
		}
	}
	orphaned := 0
	for _, audit := range h.log.audits {
		if audit.Action == "execution.orphaned" {
			orphaned++
		}
	}
	if orphaned != 3 {
		t.Errorf("audits = %+v", h.log.audits)
	}
	// An orphan that cannot be saved failed ends the poll with the error.
	another, _ := domain.NewExecution("exec-odd-2", task.ID(), bot.ID(), t0)
	_ = another.AssignTo(node.ID())
	_ = h.execs.Save(ctx, another)
	h.execs.fail = errors.New("disk full")
	if _, _, err := NewNextAssignment(h.deps).Execute(ctx, node.ID()); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("a store error: %v", err)
	}
}

func TestReconcileFailsAnExpiredLease(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	node := onlineNode(t, h, "web-1")
	task := submit(t, h, bot, "k").Task
	assignment, _, _ := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	h.clock.now = t0.Add(LeaseDuration / 2)
	if result, err := NewReconcileTasks(h.deps).Execute(ctx, 0); err != nil || len(result.Failed) != 0 {
		t.Errorf("within the lease: %v %+v", err, result)
	}
	h.clock.now = t0.Add(LeaseDuration + time.Second)
	result, err := NewReconcileTasks(h.deps).Execute(ctx, 0)
	if err != nil || len(result.Failed) != 1 || result.Failed[0] != task.ID() {
		t.Fatalf("expired lease: %v %+v", err, result)
	}
	failed, _ := h.tasks.Get(ctx, task.ID())
	execution, _ := h.execs.Get(ctx, assignment.ExecutionID)
	if failed.Status() != domain.TaskFailed || execution.Outcome() != domain.ExecutionFailed || !containsAll(failed.FailureReason(), "node web-1 stopped renewing its lease") {
		t.Errorf("task = %+v execution = %+v", failed.Snapshot(), execution.Snapshot())
	}
	// The lost run's cost is not known; it was $0.
	if !execution.Usage().CostUnknown {
		t.Errorf("usage = %+v, want an unknown cost", execution.Usage())
	}
	// A late report is not accepted and changes nothing but the cost.
	late, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), assignment.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Usage: domain.Usage{Requests: 1, CostUSD: 0.001}})
	if err != nil || late.Accepted || late.Task.Status() != domain.TaskFailed || late.Execution.Usage().CostUSD != 0.001 {
		t.Errorf("late report: %v %+v", err, late)
	}
}

func TestAssignmentPortErrors(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	node := onlineNode(t, h, "web-1")
	submit(t, h, bot, "k")
	h.tasks.listFail = errBoom
	if _, _, err := NewNextAssignment(h.deps).Execute(ctx, node.ID()); !errors.Is(err, errBoom) {
		t.Errorf("list tasks: %v", err)
	}
	h.tasks.listFail = nil
	h.execs.listFail = errBoom
	if _, _, err := NewNextAssignment(h.deps).Execute(ctx, node.ID()); !errors.Is(err, errBoom) {
		t.Errorf("list executions: %v", err)
	}
	h.execs.listFail = nil
	h.execs.fail = errBoom
	if _, _, err := NewNextAssignment(h.deps).Execute(ctx, node.ID()); !errors.Is(err, errBoom) {
		t.Errorf("save execution: %v", err)
	}
	h.execs.fail = nil
	h.log.fail = errBoom
	if _, _, err := NewNextAssignment(h.deps).Execute(ctx, node.ID()); !errors.Is(err, errBoom) {
		t.Errorf("event: %v", err)
	}
	h.log.fail = nil
	h.nodes.listFail = errBoom
	if _, _, err := NewRunNextPending(h.deps).Execute(ctx); !errors.Is(err, errBoom) {
		t.Errorf("run next list nodes: %v", err)
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}

func TestDedicatedNodeTakesOnlyTheBotsPlacedOnIt(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	anyNode := enabledBot(t, h)
	d := sampleDefinition()
	d.Name = "beside"
	d.Placement = domain.PlacementSnapshot{Selector: map[string]string{"app": "svc"}}
	placed, err := NewRegisterBot(h.deps).Execute(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	placed, _ = NewEnableBot(h.deps).Execute(ctx, placed.ID())
	registration := registration("svc-0")
	registration.Labels = map[string]string{"app": "svc"}
	registration.Capabilities.Dedicated = true
	sidecar, err := NewRegisterNode(h.deps).Execute(ctx, registration)
	if err != nil || !sidecar.Capabilities().Dedicated {
		t.Fatalf("register: %+v %v", sidecar.Snapshot(), err)
	}
	h.clock.now = t0.Add(time.Second)
	submit(t, h, anyNode, "k-any")
	h.clock.now = t0.Add(2 * time.Second)
	wanted := submit(t, h, placed, "k-placed").Task
	// The older task is the any-node bot's: the sidecar skips it.
	assignment, found, err := NewNextAssignment(h.deps).Execute(ctx, sidecar.ID())
	if err != nil || !found || assignment.Run.TaskID != wanted.ID() {
		t.Fatalf("assignment = %+v %v %v", assignment, found, err)
	}
	if _, found, _ := NewNextAssignment(h.deps).Execute(ctx, sidecar.ID()); found {
		t.Error("the sidecar took the any-node bot's task")
	}
	// No other node is Online: the any-node task runs in process.
	if _, ran, err := NewRunNextPending(h.deps).Execute(ctx); err != nil || !ran {
		t.Errorf("in-process run with only a dedicated node: %v %v", err, ran)
	}
}

// An operator cancels the task between the control plane creating the
// execution and leasing it to the node: the node is given nothing to run,
// and that is not an error of the node's poll (25 Oct 2026: the node
// logged "poll: execution … is Cancelled: cannot run again").
func TestNextAssignmentGivesNothingWhenTheTaskIsCancelledMeanwhile(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	node := onlineNode(t, h, "web-1")
	task := submit(t, h, bot, "k-cancel").Task
	h.execs.beforeSave = func(e domain.Execution) {
		if _, stored := h.execs.executions[e.ID()]; !stored {
			return
		}
		h.execs.beforeSave = nil
		if _, err := NewCancelTask(h.deps).Execute(ctx, task.ID()); err != nil {
			t.Fatalf("cancel: %v", err)
		}
	}
	assignment, found, err := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	if err != nil || found || assignment.Run != nil {
		t.Fatalf("assignment: %v %v %+v", err, found, assignment)
	}
	if got, _ := h.tasks.Get(ctx, task.ID()); got.Status() != domain.TaskCancelled {
		t.Errorf("task %s", got.Status())
	}
}

// A Pending task, or an execution queued for the node, whose bot is gone
// does not keep the node from its other work (QA 5 Nov 2026: one such
// task failed every poll of every node).
func TestNextAssignmentPassesOverAMissingBot(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	d := sampleDefinition()
	d.Name = "gone-one"
	gone, err := NewRegisterBot(h.deps).Execute(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	gone, _ = NewEnableBot(h.deps).Execute(ctx, gone.ID())
	bot := enabledBot(t, h)
	node := onlineNode(t, h, "web-1")
	h.clock.now = t0
	submit(t, h, gone, "k-gone")
	// A decision queued for the node on a Running task of the same bot.
	running := submit(t, h, gone, "k-running").Task
	queued, _ := domain.NewExecution("exec-queued", running.ID(), gone.ID(), t0)
	_ = queued.Decide("approve")
	_ = queued.AssignTo(node.ID())
	_ = h.execs.Save(ctx, queued)
	_ = running.Start("exec-queued", t0)
	_ = h.tasks.Save(ctx, running)
	h.clock.now = t0.Add(time.Minute)
	waiting := submit(t, h, bot, "k-waiting").Task
	delete(h.bots.bots, gone.ID())
	assignment, found, err := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	if err != nil || !found || assignment.Run == nil || assignment.Run.TaskID != waiting.ID() {
		t.Fatalf("the other task was not assigned: %v %v %+v", err, found, assignment)
	}
	if failed, _ := h.execs.Get(ctx, "exec-queued"); failed.Outcome() != domain.ExecutionFailed || !strings.HasPrefix(failed.FailureReason(), "orphan: ") {
		t.Errorf("queued execution = %s %q", failed.Outcome(), failed.FailureReason())
	}
}

// A decision queued for a node busy with another task says so in its
// decided event: the node carries one execution at a time and polls only
// when it is free, and the task showed Running with no hint. One queued
// for an Offline node, or a node whose record is gone, says that; a free
// node adds nothing.
func TestAQueuedDecisionSaysWhatItWaitsFor(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	node := onlineNode(t, h, "web-1")
	waiting := func(key string) domain.Task {
		t.Helper()
		task := submit(t, h, bot, key).Task
		first, _, _ := NewNextAssignment(h.deps).Execute(ctx, node.ID())
		pending := ExecutionReport{Outcome: domain.ExecutionDecisionRequired, Result: domain.Result{SessionID: "sess-" + key, PendingDecision: &domain.PendingDecision{Tool: "local_write", CallID: "c1", SessionID: "sess-" + key}}}
		if _, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), first.ExecutionID, pending); err != nil {
			t.Fatal(err)
		}
		return task
	}
	decided := func(task domain.Task) domain.Event {
		t.Helper()
		h.log.events = nil
		if _, err := NewApproveTask(h.deps).Execute(ctx, task.ID(), DecisionApprove, ""); err != nil {
			t.Fatal(err)
		}
		return h.log.events[len(h.log.events)-1]
	}
	free, other, offline, broken, gone := waiting("k-free"), waiting("k-other"), waiting("k-offline"), waiting("k-broken"), waiting("k-gone")
	if event := decided(free); event.Payload["waiting"] != nil || event.Payload["busyWith"] != nil {
		t.Errorf("a free node: %+v", event.Payload)
	}
	if _, found, _ := NewNextAssignment(h.deps).Execute(ctx, node.ID()); !found {
		t.Fatal("the node took nothing")
	}
	// The node now runs the first task's decision; the next one waits.
	event := decided(other)
	busy, _ := event.Payload["busyWith"].([]string)
	if len(busy) != 1 || busy[0] != free.ID().String() || event.Payload["waiting"] != "waiting for node web-1, busy with task "+free.ID().String() {
		t.Errorf("a busy node: %+v", event.Payload)
	}
	if last := h.log.audits[len(h.log.audits)-1]; last.Details["waiting"] != event.Payload["waiting"] {
		t.Errorf("audit = %+v", last)
	}
	h.clock.now = t0.Add(3 * time.Minute)
	if _, err := NewMarkOfflineNodes(h.deps).Execute(ctx, time.Minute); err != nil {
		t.Fatal(err)
	}
	if event := decided(offline); event.Payload["nodeStatus"] != "Offline" || event.Payload["waiting"] != "waiting for node web-1, which is Offline" {
		t.Errorf("an Offline node: %+v", event.Payload)
	}
	h.nodes.getFail = errBoom
	if _, err := NewApproveTask(h.deps).Execute(ctx, broken.ID(), DecisionApprove, ""); !errors.Is(err, errBoom) {
		t.Errorf("a failing node store: %v", err)
	}
	if stored, _ := h.tasks.Get(ctx, broken.ID()); stored.Status() != domain.TaskWaitingApproval {
		t.Errorf("a failing node store left the task %s", stored.Status())
	}
	h.nodes.getFail = nil
	delete(h.nodes.nodes, "web-1")
	if event := decided(gone); event.Payload["waiting"] != "waiting for node web-1, which is gone" {
		t.Errorf("a gone node: %+v", event.Payload)
	}
}
