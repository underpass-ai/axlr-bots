package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/underpass-bots/domain"
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
	decided, err := NewApproveTask(h.deps).Execute(ctx, task.ID(), DecisionApprove)
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

func TestNextAssignmentRefusesABrokenQueuedDecision(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	node := onlineNode(t, h, "web-1")
	task := submit(t, h, bot, "k").Task
	// An execution assigned to the node that claims to decide, but its
	// task has no earlier execution with a pending call.
	execution, _ := domain.NewExecution("exec-odd", task.ID(), bot.ID(), t0)
	_ = execution.Decide("approve")
	_ = execution.AssignTo(node.ID())
	_ = h.execs.Save(ctx, execution)
	stored, _ := h.tasks.Get(ctx, task.ID())
	_ = stored.Start("exec-odd", t0)
	_ = h.tasks.Save(ctx, stored)
	var serr domain.StateError
	if _, _, err := NewNextAssignment(h.deps).Execute(ctx, node.ID()); !errors.As(err, &serr) {
		t.Errorf("broken decision: %v", err)
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
	// A late report is accepted and changes nothing.
	late, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), assignment.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted})
	if err != nil || late.Accepted || late.Task.Status() != domain.TaskFailed {
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
