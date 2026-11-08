package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// leftRunning stands for a crash: the task and its execution are saved
// as Running at startedAt and nothing ends them.
func leftRunning(t *testing.T, h *harness, bot domain.Bot, key string, startedAt time.Time) (domain.Task, domain.Execution) {
	t.Helper()
	ctx := context.Background()
	task := submit(t, h, bot, key).Task
	executionID := domain.ExecutionID("exec-" + key)
	if err := task.Start(executionID, startedAt); err != nil {
		t.Fatal(err)
	}
	execution, err := domain.NewExecution(executionID, task.ID(), bot.ID(), startedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.execs.Save(ctx, execution); err != nil {
		t.Fatal(err)
	}
	if err := h.tasks.Save(ctx, task); err != nil {
		t.Fatal(err)
	}
	h.log.events, h.log.audits = nil, nil
	return task, execution
}

func TestReconcileTasksFailsTheStaleAndSkipsTheYoung(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h) // task timeout 900s: default grace 16 minutes
	stale, staleExec := leftRunning(t, h, bot, "old", t0)
	young, _ := leftRunning(t, h, bot, "young", t0.Add(10*time.Minute))
	h.traces.paths[staleExec.ID()] = "/traces/exec-old.jsonl"
	h.clock.now = t0.Add(17 * time.Minute)

	res, err := NewReconcileTasks(h.deps).Execute(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failed) != 1 || res.Failed[0] != stale.ID() || len(res.Skipped) != 1 || res.Skipped[0] != young.ID() {
		t.Fatalf("result = %+v", res)
	}
	failed, _ := h.tasks.Get(ctx, stale.ID())
	want := "reconciled: the control plane found it Running after 17m0s; the process that ran it is gone"
	if failed.Status() != domain.TaskFailed || failed.FailureReason() != want {
		t.Errorf("task = %s %q", failed.Status(), failed.FailureReason())
	}
	execution, _ := h.execs.Get(ctx, staleExec.ID())
	if execution.Outcome() != domain.ExecutionFailed || execution.FailureReason() != want || execution.TracePath() != "/traces/exec-old.jsonl" || !execution.EndedAt().Equal(h.clock.now) {
		t.Errorf("execution = %+v", execution.Snapshot())
	}
	left, _ := h.tasks.Get(ctx, young.ID())
	if left.Status() != domain.TaskRunning {
		t.Errorf("young task = %s", left.Status())
	}
	if got := h.log.eventTypes(); len(got) != 1 || got[0] != "bot.task.failed" {
		t.Errorf("events = %v", got)
	}
	event := h.log.events[0]
	if event.TaskID != stale.ID() || event.BotID != bot.ID() || event.CorrelationID != "trace-abc" || event.Payload["executionId"] != staleExec.ID().String() || event.Payload["reason"] != "reconciled" {
		t.Errorf("event = %+v", event)
	}
	if len(h.log.audits) != 1 || h.log.audits[0].Action != "task.reconcile" || h.log.audits[0].Subject != stale.ID().String() || h.log.audits[0].Outcome != domain.AuditOK || h.log.audits[0].Details["age"] != "17m0s" {
		t.Errorf("audits = %+v", h.log.audits)
	}

	// Running again changes nothing: the failed task is no longer Running.
	again, err := NewReconcileTasks(h.deps).Execute(ctx, 0)
	if err != nil || len(again.Failed) != 0 || len(again.Skipped) != 1 {
		t.Errorf("second run = %+v %v", again, err)
	}
	// An explicit grace overrides the bot's; a trace nobody kept stays empty.
	short, err := NewReconcileTasks(h.deps).Execute(ctx, 5*time.Minute)
	if err != nil || len(short.Failed) != 1 || short.Failed[0] != young.ID() {
		t.Errorf("short grace = %+v %v", short, err)
	}
	execution, _ = h.execs.Get(ctx, "exec-young")
	if execution.Outcome() != domain.ExecutionFailed || execution.TracePath() != "" {
		t.Errorf("young execution = %+v", execution.Snapshot())
	}
	// Two submissions, two starts, two reconciliations: skipping saves nothing.
	if h.tasks.saves != 6 {
		t.Errorf("task saved %d times, want 6", h.tasks.saves)
	}
}

func TestReconcileTasksGraceComesFromTheBot(t *testing.T) {
	ctx := context.Background()
	withTimeout := sampleDefinition()
	withTimeout.Budgets.TaskTimeoutSeconds = 60
	withoutTimeout := sampleDefinition()
	withoutTimeout.Name = "no-timeout"
	withoutTimeout.Budgets = BudgetsDefinition{}
	cases := []struct {
		name       string
		definition BotDefinition
		age        time.Duration
		failed     bool
	}{
		{"older than timeout plus a minute", withTimeout, 2*time.Minute + time.Second, true},
		{"exactly the grace is still within it", withTimeout, 2 * time.Minute, false},
		{"no timeout falls back to the default", withoutTimeout, 3 * time.Minute, false},
		{"no timeout, older than the default", withoutTimeout, 16*time.Minute + time.Second, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness()
			bot, err := NewRegisterBot(h.deps).Execute(ctx, c.definition)
			if err != nil {
				t.Fatal(err)
			}
			bot, _ = NewEnableBot(h.deps).Execute(ctx, bot.ID())
			task, _ := leftRunning(t, h, bot, "k", t0)
			h.clock.now = t0.Add(c.age)
			res, err := NewReconcileTasks(h.deps).Execute(ctx, 0)
			if err != nil {
				t.Fatal(err)
			}
			if (len(res.Failed) == 1) != c.failed || len(res.Failed)+len(res.Skipped) != 1 {
				t.Errorf("result = %+v", res)
			}
			got, _ := h.tasks.Get(ctx, task.ID())
			if (got.Status() == domain.TaskFailed) != c.failed {
				t.Errorf("task = %s", got.Status())
			}
		})
	}
}

func TestReconcileTasksIgnoresTheBotStatus(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	task, _ := leftRunning(t, h, bot, "k", t0)
	paused := bot
	_ = paused.Pause(t0)
	_ = h.bots.Save(ctx, paused)
	h.clock.now = t0.Add(time.Hour)
	res, err := NewReconcileTasks(h.deps).Execute(ctx, 0)
	if err != nil || len(res.Failed) != 1 || res.Failed[0] != task.ID() {
		t.Errorf("paused bot: %+v %v", res, err)
	}
}

func TestReconcileTasksNamesAnExecutionThatHadEnded(t *testing.T) {
	// The process died after saving the execution as ended and before
	// saving the task: the task is Running, the execution is not.
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	task, execution := leftRunning(t, h, bot, "k", t0)
	_ = execution.Complete(domain.Result{Answer: "done"}, domain.Usage{}, "/traces/exec-k.jsonl", t0.Add(time.Minute))
	_ = h.execs.Save(ctx, execution)
	h.clock.now = t0.Add(time.Hour)
	res, err := NewReconcileTasks(h.deps).Execute(ctx, 0)
	if err != nil || len(res.Failed) != 1 {
		t.Fatalf("result = %+v %v", res, err)
	}
	got, _ := h.tasks.Get(ctx, task.ID())
	if got.Status() != domain.TaskFailed || !strings.Contains(got.FailureReason(), "although its execution exec-k had already ended Completed") {
		t.Errorf("task = %s %q", got.Status(), got.FailureReason())
	}
	kept, _ := h.execs.Get(ctx, execution.ID())
	if kept.Outcome() != domain.ExecutionCompleted || kept.Result().Answer != "done" {
		t.Errorf("the ended execution was changed: %+v", kept.Snapshot())
	}
	if got := h.log.eventTypes(); len(got) != 1 || got[0] != "bot.task.failed" {
		t.Errorf("events = %v", got)
	}
}

func TestReconcileTasksPortErrors(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	if res, err := NewReconcileTasks(h.deps).Execute(ctx, 0); err != nil || len(res.Failed) != 0 || len(res.Skipped) != 0 {
		t.Errorf("empty store: %+v %v", res, err)
	}
	h.tasks.listFail = errBoom
	if _, err := NewReconcileTasks(h.deps).Execute(ctx, 0); !errors.Is(err, errBoom) {
		t.Errorf("list error: %v", err)
	}
	h.tasks.listFail = nil

	// A Running task whose bot vanished is skipped, not a failed pass
	// (QA L11), then one whose execution vanished.
	orphan, _ := domain.NewTask("orphan", "ghost", "o", "k", "", t0)
	_ = orphan.Start("exec-ghost", t0)
	_ = h.tasks.Save(ctx, orphan)
	if res, err := NewReconcileTasks(h.deps).Execute(ctx, 0); err != nil || len(res.Skipped) != 1 || res.Skipped[0] != "orphan" {
		t.Errorf("missing bot: %+v %v", res, err)
	}
	h.tasks.tasks = nil
	bot := enabledBot(t, h)
	lost := submit(t, h, bot, "lost").Task
	_ = lost.Start("exec-missing", t0)
	_ = h.tasks.Save(ctx, lost)
	if _, err := NewReconcileTasks(h.deps).Execute(ctx, 0); !domain.IsNotFound(err) {
		t.Errorf("missing execution: %v", err)
	}

	for name, arrange := range map[string]func(*harness){
		"execution save": func(h *harness) { h.execs.fail = errBoom },
		"task save":      func(h *harness) { h.tasks.fail = errBoom },
		"publish":        func(h *harness) { h.log.fail = errBoom },
		"audit":          func(h *harness) { h.log.recordFail = errBoom },
	} {
		h := newHarness()
		bot := enabledBot(t, h)
		leftRunning(t, h, bot, "k", t0)
		h.clock.now = t0.Add(time.Hour)
		arrange(h)
		if _, err := NewReconcileTasks(h.deps).Execute(ctx, 0); !errors.Is(err, errBoom) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// runningExecution saves a Running execution of the task that no task
// save followed: what a crash between the two saves leaves.
func runningExecution(t *testing.T, h *harness, id domain.ExecutionID, task domain.TaskID, bot domain.BotID, startedAt time.Time) {
	t.Helper()
	execution, err := domain.NewExecution(id, task, bot, startedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.execs.Save(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
}

// A Running execution its task does not run, for a task still Pending,
// one already Completed or one that is gone, is failed once older than
// the grace; the tasks are left as they are and a younger one waits.
func TestReconcileTasksFailsTheOrphanedExecutions(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	pending := submit(t, h, bot, "pending").Task
	completed, err := NewRunTask(h.deps).Execute(ctx, submit(t, h, bot, "completed").Task.ID())
	if err != nil || completed.Task.Status() != domain.TaskCompleted {
		t.Fatalf("run: %v %s", err, completed.Task.Status())
	}
	runningExecution(t, h, "exec-a-orphan", pending.ID(), bot.ID(), t0)
	runningExecution(t, h, "exec-b-orphan", completed.Task.ID(), bot.ID(), t0)
	runningExecution(t, h, "exec-c-gone", "task-gone", bot.ID(), t0)
	runningExecution(t, h, "exec-d-young", pending.ID(), bot.ID(), t0.Add(10*time.Minute))
	h.clock.now = t0.Add(17 * time.Minute)
	result, err := NewReconcileTasks(h.deps).Execute(ctx, 0)
	if err != nil || len(result.Failed) != 0 || len(result.Orphaned) != 3 || result.Orphaned[0] != "exec-a-orphan" || result.Orphaned[1] != "exec-b-orphan" || result.Orphaned[2] != "exec-c-gone" {
		t.Fatalf("result = %+v %v", result, err)
	}
	for id, want := range map[domain.ExecutionID]string{"exec-a-orphan": "is Pending and does not run it", "exec-b-orphan": "is Completed and does not run it", "exec-c-gone": "task-gone is gone"} {
		execution, _ := h.execs.Get(ctx, id)
		if execution.Outcome() != domain.ExecutionFailed || !strings.HasPrefix(execution.FailureReason(), "reconciled: orphan: ") || !strings.HasSuffix(execution.FailureReason(), want) {
			t.Errorf("%s = %s %q", id, execution.Outcome(), execution.FailureReason())
		}
	}
	if young, _ := h.execs.Get(ctx, "exec-d-young"); young.Outcome() != domain.ExecutionRunning {
		t.Errorf("the young one = %s", young.Outcome())
	}
	if got, _ := h.tasks.Get(ctx, pending.ID()); got.Status() != domain.TaskPending {
		t.Errorf("pending = %s", got.Status())
	}
	if got, _ := h.tasks.Get(ctx, completed.Task.ID()); got.Status() != domain.TaskCompleted {
		t.Errorf("completed = %s", got.Status())
	}
	audits := 0
	for _, audit := range h.log.audits {
		if audit.Action == "execution.orphaned" {
			audits++
		}
	}
	if audits != 3 {
		t.Errorf("audits = %+v", h.log.audits)
	}
	// The pass stops at a store error.
	h.clock.now = t0.Add(time.Hour)
	h.execs.fail = errBoom
	if _, err := NewReconcileTasks(h.deps).Execute(ctx, 0); !errors.Is(err, errBoom) {
		t.Errorf("save: %v", err)
	}
	h.execs.fail, h.execs.listFail = nil, errBoom
	if _, err := NewReconcileTasks(h.deps).Execute(ctx, 0); !errors.Is(err, errBoom) {
		t.Errorf("list: %v", err)
	}
}

// A node's report lands while reconcile fails the same execution: the
// report's Completed stands, and the task ends once, Completed.
func TestReconcileTasksLeavesAReportThatLandsMeanwhile(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	node := onlineNode(t, h, "web-1")
	task := submit(t, h, bot, "k").Task
	assignment, _, _ := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	h.clock.now = t0.Add(LeaseDuration + time.Second)
	h.execs.beforeSave = func(e domain.Execution) {
		if e.Outcome() != domain.ExecutionFailed {
			return
		}
		h.execs.beforeSave = nil
		if _, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), assignment.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done"}}); err != nil {
			t.Fatalf("report: %v", err)
		}
	}
	result, err := NewReconcileTasks(h.deps).Execute(ctx, 0)
	if err != nil || len(result.Failed) != 0 {
		t.Fatalf("reconcile = %+v %v", result, err)
	}
	got, _ := h.tasks.Get(ctx, task.ID())
	execution, _ := h.execs.Get(ctx, assignment.ExecutionID)
	if got.Status() != domain.TaskCompleted || execution.Outcome() != domain.ExecutionCompleted {
		t.Errorf("task %s, execution %s", got.Status(), execution.Outcome())
	}
	ends := 0
	for _, event := range h.log.events {
		if event.TaskID == task.ID() && (event.Type == domain.EventTaskCompleted || event.Type == domain.EventTaskFailed) {
			ends++
		}
	}
	if ends != 1 {
		t.Errorf("events = %v", h.log.eventTypes())
	}
	// A report the other way round: reconcile ended it first, and the
	// report that read it Running is answered as late.
	other := submit(t, h, bot, "k-2").Task
	second, _, _ := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	h.clock.now = h.clock.now.Add(LeaseDuration + time.Second)
	h.execs.beforeSave = func(e domain.Execution) {
		if e.Outcome() != domain.ExecutionCompleted {
			return
		}
		h.execs.beforeSave = nil
		if _, err := NewReconcileTasks(h.deps).Execute(ctx, 0); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
	}
	late, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), second.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted})
	if err != nil || late.Accepted || late.Execution.Outcome() != domain.ExecutionFailed || late.Task.ID() != other.ID() || late.Task.Status() != domain.TaskFailed {
		t.Errorf("late = %v %+v", err, late)
	}
}

// A task the pass listed Running that a cancel ended before its save is
// left as the cancel left it.
func TestReconcileTasksRereadsBeforeItSaves(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	listed, _ := leftRunning(t, h, bot, "k", t0)
	if _, err := NewCancelTask(h.deps).Execute(ctx, listed.ID()); err != nil {
		t.Fatal(err)
	}
	h.clock.now = t0.Add(time.Hour)
	if failed, err := NewReconcileTasks(h.deps).reconcile(ctx, listed, 0); err != nil || failed {
		t.Errorf("reconcile = %v %v", failed, err)
	}
	if got, _ := h.tasks.Get(ctx, listed.ID()); got.Status() != domain.TaskCancelled {
		t.Errorf("task = %s", got.Status())
	}
}

// A delegated task runs until its grant's deadline, whatever its bot's
// own timeout: reconcile fails it a minute after that deadline.
func TestReconcileTasksBoundsADelegatedTaskByItsDeadline(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	onlineNode(t, h, "web-1")
	a := delegationBot(t, h, "bot-a", &domain.DelegationSnapshot{To: []string{"bot-b"}, BudgetShare: 1}, BudgetsDefinition{TaskTimeoutSeconds: 120})
	delegationBot(t, h, "bot-b", nil, BudgetsDefinition{TaskTimeoutSeconds: 900})
	parent := runOnNode(t, h, a, "web-1", "ask-1")
	h.clock.now = t0.Add(60 * time.Second)
	answer := delegate(t, h, "web-1", parent.ExecutionID, "bot-b", "Quick")
	child := nextFor(t, h, "web-1")
	if child.Run == nil || child.Run.TaskID != answer.TaskID || child.Run.Timeout != 60*time.Second {
		t.Fatalf("child = %+v", child.Run)
	}
	// The node renews the child's lease; the deadline is t0+120s.
	h.clock.now = t0.Add(150 * time.Second)
	if _, err := NewRenewLease(h.deps).Execute(ctx, "web-1", child.ExecutionID); err != nil {
		t.Fatal(err)
	}
	h.clock.now = answer.Grant.Deadline.Add(ReconcileGrace - time.Second)
	if result, err := NewReconcileTasks(h.deps).Execute(ctx, 0); err != nil || containsTask(result.Failed, answer.TaskID) {
		t.Fatalf("within a minute of the deadline: %+v %v", result, err)
	}
	h.clock.now = answer.Grant.Deadline.Add(ReconcileGrace + time.Second)
	if result, err := NewReconcileTasks(h.deps).Execute(ctx, 0); err != nil || !containsTask(result.Failed, answer.TaskID) {
		t.Fatalf("a minute past the deadline: %+v %v", result, err)
	}
	if got, _ := h.tasks.Get(ctx, answer.TaskID); got.Status() != domain.TaskFailed || !strings.HasPrefix(got.FailureReason(), "reconciled: ") {
		t.Errorf("child = %s %q", got.Status(), got.FailureReason())
	}
}

// A child with less than a second left is not handed to a node, which
// would read a zero timeout as none, nor run in process: it ends by its
// deadline instead.
func TestAChildWithUnderASecondLeftIsNotRun(t *testing.T) {
	for name, run := range map[string]func(*harness, domain.TaskID) error{
		"on a node": func(h *harness, _ domain.TaskID) error {
			if assignment, found, err := NewNextAssignment(h.deps).Execute(context.Background(), "web-1"); err != nil || found {
				return fmt.Errorf("assigned: %v %v %+v", err, found, assignment)
			}
			return nil
		},
		"in process": func(h *harness, id domain.TaskID) error {
			if _, err := NewRunTask(h.deps).Execute(context.Background(), id); !isState(err) {
				return fmt.Errorf("ran: %v", err)
			}
			return nil
		},
	} {
		h := newHarness()
		onlineNode(t, h, "web-1")
		a := delegationBot(t, h, "bot-a", &domain.DelegationSnapshot{To: []string{"bot-b"}, BudgetShare: 1}, BudgetsDefinition{TaskTimeoutSeconds: 120})
		delegationBot(t, h, "bot-b", nil, BudgetsDefinition{TaskTimeoutSeconds: 900})
		parent := runOnNode(t, h, a, "web-1", "ask-1")
		h.clock.now = t0.Add(60 * time.Second)
		answer := delegate(t, h, "web-1", parent.ExecutionID, "bot-b", "Quick")
		h.clock.now = answer.Grant.Deadline.Add(-500 * time.Millisecond)
		if err := run(h, answer.TaskID); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if got, _ := h.tasks.Get(context.Background(), answer.TaskID); got.Status() != domain.TaskFailed || !strings.HasPrefix(got.FailureReason(), "deadline:") {
			t.Errorf("%s: child = %s %q", name, got.Status(), got.FailureReason())
		}
	}
}

// queuedDecision leaves a task Running on a decision queued for web-1,
// as an approval (or its deadline's deny) leaves it: not yet leased.
func queuedDecision(t *testing.T, h *harness) (domain.Task, ApproveTaskResult) {
	t.Helper()
	ctx := context.Background()
	bot := enabledBot(t, h)
	node := onlineNode(t, h, "web-1")
	task := submit(t, h, bot, "k").Task
	first, _, _ := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	pending := ExecutionReport{Outcome: domain.ExecutionDecisionRequired, Result: domain.Result{SessionID: "sess-1", PendingDecision: &domain.PendingDecision{Tool: "local_write", CallID: "c1", SessionID: "sess-1"}}}
	if _, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), first.ExecutionID, pending); err != nil {
		t.Fatal(err)
	}
	decided, err := NewApproveTask(h.deps).Execute(ctx, task.ID(), DecisionDeny, "", "")
	if err != nil || !decided.Execution.LeaseUntil().IsZero() {
		t.Fatalf("decided = %+v %v", decided.Execution.Snapshot(), err)
	}
	return task, decided
}

// offlineEvents are the node.offline events in the task's timeline.
func offlineEvents(h *harness, task domain.TaskID) []domain.Event {
	var out []domain.Event
	for _, e := range h.log.events {
		if e.Type == domain.EventNodeOffline && e.TaskID == task {
			out = append(out, e)
		}
	}
	return out
}

// A decision queued for a node that went Offline waits for it (a reboot
// or a reinstall takes minutes and the run resumes), and the task's
// timeline says so once per state of the node: before, the task showed
// Running with no hint (QA L9). Only the timeout fails it, naming the
// node.
func TestReconcileTellsADecisionQueuedForAnOfflineNodeAndKeepsIt(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	task, decided := queuedDecision(t, h)
	uc := NewReconcileTasks(h.deps)
	h.clock.now = t0.Add(5 * time.Minute)
	if result, err := uc.Execute(ctx, 0); err != nil || len(result.Failed) != 0 || len(offlineEvents(h, task.ID())) != 0 {
		t.Fatalf("an Online node's decision: %+v %v", result, err)
	}
	away := func() domain.Node {
		t.Helper()
		if _, err := NewMarkOfflineNodes(h.deps).Execute(ctx, time.Minute); err != nil {
			t.Fatal(err)
		}
		node, _ := h.nodes.Get(ctx, "web-1")
		if node.IsOnline() {
			t.Fatal("the node is still Online")
		}
		return node
	}
	node := away()
	for pass := 0; pass < 3; pass++ {
		h.clock.now = h.clock.now.Add(time.Minute)
		if result, err := uc.Execute(ctx, 0); err != nil || len(result.Failed) != 0 {
			t.Fatalf("pass %d failed the waiting decision: %+v %v", pass, result, err)
		}
	}
	told := offlineEvents(h, task.ID())
	if len(told) != 1 {
		t.Fatalf("offline events = %+v, want one for the state", told)
	}
	want := "waiting for node web-1, offline since " + node.LastSeenAt().UTC().Format("15:04") + " UTC"
	if e := told[0]; e.Payload["waiting"] != want || e.Payload["nodeStatus"] != "Offline" || e.NodeID != "web-1" || e.ExecutionID != decided.Execution.ID() || e.BotID != task.BotID() {
		t.Errorf("event = %+v", e)
	}
	if stored, _ := h.tasks.Get(ctx, task.ID()); stored.Status() != domain.TaskRunning {
		t.Errorf("task = %s", stored.Status())
	}
	// Back, then away again: a new state, told again.
	if _, err := NewHeartbeatNode(h.deps).Execute(ctx, "web-1"); err != nil {
		t.Fatal(err)
	}
	h.clock.now = h.clock.now.Add(3 * time.Minute)
	away()
	if _, err := uc.Execute(ctx, 0); err != nil || len(offlineEvents(h, task.ID())) != 2 {
		t.Errorf("away again: %d events %v", len(offlineEvents(h, task.ID())), err)
	}
	// The timeout fails it, naming the node; it never ran, so it cost
	// nothing.
	h.clock.now = t0.Add(DefaultTaskTimeout + ReconcileGrace + time.Second)
	result, err := uc.Execute(ctx, 0)
	if err != nil || len(result.Failed) != 1 || result.Failed[0] != task.ID() {
		t.Fatalf("at the timeout: %+v %v", result, err)
	}
	stored, _ := h.tasks.Get(ctx, task.ID())
	execution, _ := h.execs.Get(ctx, decided.Execution.ID())
	reason := "reconciled: its decision (deny, execution " + decided.Execution.ID().String() + ") waited for node web-1, which is Offline since it was last seen at "
	if stored.Status() != domain.TaskFailed || !strings.HasPrefix(stored.FailureReason(), reason) || execution.Outcome() != domain.ExecutionFailed || execution.Usage().CostUnknown {
		t.Errorf("task = %s %q, execution = %s %+v", stored.Status(), stored.FailureReason(), execution.Outcome(), execution.Usage())
	}
}

// A queued decision whose node record is gone is told once too; a node
// store or a timeline that fails stops the pass.
func TestReconcileTellsADecisionQueuedForAGoneNode(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	task, _ := queuedDecision(t, h)
	h.nodes.getFail = errBoom
	if _, err := NewReconcileTasks(h.deps).Execute(ctx, 0); !errors.Is(err, errBoom) {
		t.Errorf("a failing node store: %v", err)
	}
	h.nodes.getFail = nil
	delete(h.nodes.nodes, "web-1")
	h.log.fail = errBoom
	if _, err := NewReconcileTasks(h.deps).Execute(ctx, 0); !errors.Is(err, errBoom) {
		t.Errorf("a failing timeline: %v", err)
	}
	h.log.fail = nil
	for pass := 0; pass < 2; pass++ {
		if result, err := NewReconcileTasks(h.deps).Execute(ctx, 0); err != nil || len(result.Failed) != 0 {
			t.Fatalf("result = %+v %v", result, err)
		}
	}
	if told := offlineEvents(h, task.ID()); len(told) != 1 || told[0].Payload["waiting"] != "waiting for node web-1, which is gone" || told[0].Payload["nodeStatus"] != "gone" {
		t.Errorf("events = %+v", told)
	}
}
