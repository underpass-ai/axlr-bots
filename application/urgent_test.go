package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

func submitWith(t *testing.T, h *harness, bot, key, priority string) domain.Task {
	t.Helper()
	result, err := NewSubmitTask(h.deps).Execute(context.Background(), SubmitTaskCommand{BotID: bot, Objective: "Do " + key, IdempotencyKey: key, Priority: priority})
	if err != nil {
		t.Fatal(err)
	}
	return result.Task
}

func taskOf(t *testing.T, h *harness, id domain.TaskID) domain.Task {
	t.Helper()
	task, err := h.tasks.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

var waitingReport = ExecutionReport{Outcome: domain.ExecutionDecisionRequired, Result: domain.Result{SessionID: "sess-1", PendingDecision: &domain.PendingDecision{Tool: "local_write", CallID: "c1", SessionID: "sess-1"}}}

func TestAnUrgentTaskPreemptsTheWorkInProgress(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	onlineNode(t, h, "web-1")
	teamBot(t, h, "quad-1", nil, nil)
	running := submitWith(t, h, "quad-1", "patrol", "")
	runningRun := nextFor(t, h, "web-1")
	waiting := submitWith(t, h, "quad-1", "survey", "")
	waitingRun := nextFor(t, h, "web-1")
	if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", waitingRun.ExecutionID, waitingReport); err != nil {
		t.Fatal(err)
	}
	h.clock.now = t0.Add(time.Second)
	queued := submitWith(t, h, "quad-1", "restock", "")
	h.clock.now = t0.Add(2 * time.Second)
	if _, err := NewSubmitTask(h.deps).Execute(ctx, SubmitTaskCommand{BotID: "quad-1", Objective: "x", IdempotencyKey: "bad", Priority: "now"}); err == nil {
		t.Error("an unknown priority was taken")
	}
	urgent := submitWith(t, h, "quad-1", "recall", "urgent")
	if !urgent.IsUrgent() {
		t.Fatal("the task is not urgent")
	}
	for _, id := range []domain.TaskID{running.ID(), waiting.ID()} {
		task := taskOf(t, h, id)
		if task.Status() != domain.TaskCancelled || !strings.Contains(task.FailureReason(), "preempted by the urgent task "+urgent.ID().String()) || !strings.Contains(task.FailureReason(), "the operator submitted it as urgent") {
			t.Errorf("%s = %s %q", id, task.Status(), task.FailureReason())
		}
	}
	if execution, _ := h.execs.Get(ctx, runningRun.ExecutionID); execution.Outcome() != domain.ExecutionCancelled {
		t.Errorf("the running execution is %s", execution.Outcome())
	}
	if wait, _ := taskOf(t, h, waiting.ID()).Approval(); wait.Open() {
		t.Error("the preempted wait is still open")
	}
	if _, err := NewApproveTask(h.deps).Execute(ctx, waiting.ID(), DecisionApprove, ""); err == nil {
		t.Error("a late approval of a preempted task was taken")
	}
	if taskOf(t, h, queued.ID()).Status() != domain.TaskPending {
		t.Error("a Pending task was preempted")
	}
	preempted := 0
	for _, e := range h.log.events {
		if e.Type == domain.EventTaskPreempted && e.Payload["preemptedBy"] == urgent.ID().String() {
			preempted++
		}
	}
	if preempted != 2 {
		t.Errorf("%d preempted events", preempted)
	}
	if submitted, ok := lastEvent(h, domain.EventTaskSubmitted); !ok || submitted.Payload["priority"] != "urgent" {
		t.Errorf("submitted = %+v", submitted)
	}
	// The urgent task goes before the older Pending one.
	if next := nextFor(t, h, "web-1"); next.Run.TaskID != urgent.ID() {
		t.Errorf("assigned %s first", next.Run.TaskID)
	}
	second := submitWith(t, h, "quad-1", "recall-again", "urgent")
	if taskOf(t, h, urgent.ID()).Status() != domain.TaskRunning || taskOf(t, h, second.ID()).Status() != domain.TaskPending {
		t.Error("an urgent task preempted another urgent one")
	}
	h.tasks.listFail = errBoom
	if _, err := NewSubmitTask(h.deps).Execute(ctx, SubmitTaskCommand{BotID: "quad-1", Objective: "x", IdempotencyKey: "fails", Priority: "urgent"}); !errors.Is(err, errBoom) {
		t.Errorf("list failure: %v", err)
	}
}

func TestATeammatesStopInterruptsAndAWarningDoesNot(t *testing.T) {
	h := newHarness()
	parent := portTeam(t, h)
	busy := submitWith(t, h, "quad-1", "patrol", "")
	nextFor(t, h, "web-1")
	warning := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "warn.ship-near", Message: "a ship", To: []string{"quad-1"}})
	if len(warning.Deliveries) != 1 || warning.Deliveries[0].Urgent || taskOf(t, h, busy.ID()).Status() != domain.TaskRunning {
		t.Fatalf("an addressed warning interrupted: %+v", warning)
	}
	recall := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "stop.recall", Message: "a ship", To: []string{"quad-1"}})
	if len(recall.Deliveries) != 1 || !recall.Deliveries[0].Urgent {
		t.Fatalf("recall = %+v", recall)
	}
	if task := taskOf(t, h, busy.ID()); task.Status() != domain.TaskCancelled || !strings.Contains(task.FailureReason(), "a critical from its teammate plane, event stop.recall") {
		t.Errorf("busy = %s %q", task.Status(), task.FailureReason())
	}
	broadcast := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "warn.ship-sighted", Message: "a ship"})
	if len(broadcast.Deliveries) != 0 {
		t.Errorf("a warning broadcast reached a bot that subscribes to nothing: %+v", broadcast)
	}
}

// autoBot is a bot whose approvals let return_home run without a
// person and notify a command otherwise.
func autoBot(t *testing.T, h *harness) domain.Bot {
	t.Helper()
	return teamBot(t, h, "quad-1", nil, func(d *BotDefinition) {
		d.Level = "act-with-approval"
		d.Approvals = domain.ApprovalsSnapshot{AutoApprove: []domain.ToolRuleSnapshot{{Tool: "return_home"}}, Notify: []domain.NotifyTargetSnapshot{{Type: "command", Command: []string{"/bin/true"}}}}
	})
}

func pendingCall(tool string, arguments string) ExecutionReport {
	return ExecutionReport{Outcome: domain.ExecutionDecisionRequired, Result: domain.Result{SessionID: "sess-1", PendingDecision: &domain.PendingDecision{Tool: tool, CallID: "c1", SessionID: "sess-1", Arguments: json.RawMessage(arguments)}}}
}

func TestReturnHomeRunsWithoutAPerson(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	onlineNode(t, h, "web-1")
	autoBot(t, h)
	home := submitWith(t, h, "quad-1", "recall", "urgent")
	run := nextFor(t, h, "web-1")
	if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", run.ExecutionID, pendingCall(domain.MCPCallTool, `{"name": "vehicle/return_home", "why": "a ship", "arguments": {"reason": "a ship"}}`)); err != nil {
		t.Fatal(err)
	}
	if task := taskOf(t, h, home.ID()); task.Status() != domain.TaskRunning {
		t.Fatalf("task = %s", task.Status())
	}
	decided, ok := lastEvent(h, domain.EventTaskDecided)
	if !ok || decided.Payload["decidedBy"] != PolicyActor || decided.Payload["decision"] != DecisionApprove {
		t.Errorf("decided = %+v", decided)
	}
	if pending, _ := h.outbox.ListPending(ctx); len(pending) != 0 {
		t.Errorf("a person was notified: %d", len(pending))
	}
	if next := nextFor(t, h, "web-1"); next.Decide == nil || next.Decide.Decision != DecisionApprove {
		t.Errorf("the decision was not queued: %+v", next)
	}
	// Taking off is a person's decision.
	fly := submitWith(t, h, "quad-1", "fly", "")
	flyRun := nextFor(t, h, "web-1")
	if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", flyRun.ExecutionID, pendingCall(domain.MCPCallTool, `{"name": "run_mission", "arguments": {}}`)); err != nil {
		t.Fatal(err)
	}
	if task := taskOf(t, h, fly.ID()); task.Status() != domain.TaskWaitingApproval {
		t.Errorf("fly = %s", task.Status())
	}
	if pending, _ := h.outbox.ListPending(ctx); len(pending) != 1 {
		t.Errorf("deliveries = %d", len(pending))
	}
}

func TestAnAutoApprovalThatCannotRunAsksAPerson(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	onlineNode(t, h, "web-1")
	bot := autoBot(t, h)
	home := submitWith(t, h, "quad-1", "recall", "")
	run := nextFor(t, h, "web-1")
	if _, err := NewPauseBot(h.deps).Execute(ctx, bot.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", run.ExecutionID, pendingCall("return_home", `{}`)); err != nil {
		t.Fatal(err)
	}
	if task := taskOf(t, h, home.ID()); task.Status() != domain.TaskWaitingApproval {
		t.Errorf("task = %s", task.Status())
	}
	failed := false
	for _, a := range h.log.audits {
		failed = failed || (a.Action == "task.decide" && a.Outcome == domain.AuditFailed && a.Details["actor"] == PolicyActor)
	}
	if !failed {
		t.Error("the failed auto-approval is not audited")
	}
}

func TestStopPausesTheBotAndCancelsItsWork(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	onlineNode(t, h, "web-1")
	teamBot(t, h, "quad-1", nil, nil)
	done := submitWith(t, h, "quad-1", "done", "")
	doneRun := nextFor(t, h, "web-1")
	if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", doneRun.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "ok"}}); err != nil {
		t.Fatal(err)
	}
	running := submitWith(t, h, "quad-1", "running", "")
	nextFor(t, h, "web-1")
	waiting := submitWith(t, h, "quad-1", "waiting", "")
	waitingRun := nextFor(t, h, "web-1")
	if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", waitingRun.ExecutionID, waitingReport); err != nil {
		t.Fatal(err)
	}
	pending := submitWith(t, h, "quad-1", "pending", "")
	result, err := NewStopBot(h.deps).Execute(ctx, "quad-1")
	if err != nil || result.Bot.Status() != domain.BotPaused || len(result.Cancelled) != 3 || len(result.NotCancelled) != 0 {
		t.Fatalf("stop = %+v %v", result, err)
	}
	for _, id := range []domain.TaskID{running.ID(), waiting.ID(), pending.ID()} {
		if task := taskOf(t, h, id); task.Status() != domain.TaskCancelled || task.FailureReason() != "stopped by test:operator" {
			t.Errorf("%s = %s %q", id, task.Status(), task.FailureReason())
		}
	}
	if taskOf(t, h, done.ID()).Status() != domain.TaskCompleted {
		t.Error("a finished task changed")
	}
	again, err := NewStopBot(h.deps).Execute(ctx, "quad-1")
	if err != nil || len(again.Cancelled) != 0 || again.Bot.Status() != domain.BotPaused {
		t.Errorf("again = %+v %v", again, err)
	}
	if _, err := NewStopBot(h.deps).Execute(ctx, "ghost"); !domain.IsNotFound(err) {
		t.Errorf("ghost: %v", err)
	}
	h.tasks.listFail = errBoom
	if _, err := NewStopBot(h.deps).Execute(ctx, "quad-1"); !errors.Is(err, errBoom) {
		t.Errorf("list failure: %v", err)
	}
}

func TestCancelKeepsWhoCancelled(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	teamBot(t, h, "quad-1", nil, nil)
	task := submitWith(t, h, "quad-1", "x", "")
	result, err := NewCancelTask(h.deps).Execute(ctx, task.ID())
	if err != nil || result.Task.FailureReason() != "cancelled by test:operator" {
		t.Errorf("cancel = %+v %v", result.Task.Snapshot(), err)
	}
}

func TestTellingTheTeamNeedsNoPerson(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	onlineNode(t, h, "web-1")
	teamBot(t, h, "plane", []string{"port"}, func(d *BotDefinition) { d.Level = "act-with-approval" })
	task := submitWith(t, h, "plane", "warn", "")
	run := nextFor(t, h, "web-1")
	call := pendingCall(domain.MCPCallTool, `{"name": "axlr-bots/axlr_bots_emit_event", "arguments": {"name": "recall", "message": "a ship"}}`)
	if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", run.ExecutionID, call); err != nil {
		t.Fatal(err)
	}
	decided, ok := lastEvent(h, domain.EventTaskDecided)
	if !ok || decided.Payload["decidedBy"] != TeamEventActor || taskOf(t, h, task.ID()).Status() != domain.TaskRunning {
		t.Errorf("decided = %+v", decided)
	}
}
