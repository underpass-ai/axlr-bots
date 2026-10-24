package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/underpass-bots/domain"
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

	// A Running task whose bot vanished, then one whose execution vanished.
	orphan, _ := domain.NewTask("orphan", "ghost", "o", "k", "", t0)
	_ = orphan.Start("exec-ghost", t0)
	_ = h.tasks.Save(ctx, orphan)
	if _, err := NewReconcileTasks(h.deps).Execute(ctx, 0); !domain.IsNotFound(err) || !strings.Contains(err.Error(), "reconcile task orphan") {
		t.Errorf("missing bot: %v", err)
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
