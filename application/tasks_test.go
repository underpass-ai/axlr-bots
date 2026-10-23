package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/underpass-ai/underpass-bots/domain"
)

func enabledBot(t *testing.T, h *harness) domain.Bot {
	t.Helper()
	ctx := context.Background()
	bot, err := NewRegisterBot(h.deps).Execute(ctx, sampleDefinition())
	if err != nil {
		t.Fatal(err)
	}
	bot, err = NewEnableBot(h.deps).Execute(ctx, bot.ID())
	if err != nil {
		t.Fatal(err)
	}
	h.log.events, h.log.audits = nil, nil
	return bot
}

func submit(t *testing.T, h *harness, bot domain.Bot, key string) SubmitTaskResult {
	t.Helper()
	res, err := NewSubmitTask(h.deps).Execute(context.Background(), SubmitTaskCommand{
		BotID: bot.ID().String(), Objective: "Investigate an HTTP 500 spike", IdempotencyKey: key, CorrelationID: "trace-abc",
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestSubmitTaskIsIdempotentOnTheKey(t *testing.T) {
	h := newHarness()
	bot := enabledBot(t, h)
	first := submit(t, h, bot, "alert-1")
	if !first.Created || first.Task.Status() != domain.TaskPending || first.Task.CorrelationID() != "trace-abc" {
		t.Fatalf("first = %+v", first.Task.Snapshot())
	}
	again := submit(t, h, bot, "alert-1")
	if again.Created || again.Task.ID() != first.Task.ID() {
		t.Errorf("resubmission created %v / %s vs %s", again.Created, again.Task.ID(), first.Task.ID())
	}
	if h.tasks.saves != 1 {
		t.Errorf("task saved %d times, want 1", h.tasks.saves)
	}
	if got := h.log.eventTypes(); len(got) != 1 || got[0] != "bot.task.submitted" {
		t.Errorf("events = %v", got)
	}
	if len(h.log.audits) != 2 || h.log.audits[1].Details["deduplicated"] != "true" {
		t.Errorf("audits = %+v", h.log.audits)
	}
	other := submit(t, h, bot, "alert-2")
	if !other.Created || other.Task.ID() == first.Task.ID() {
		t.Error("a new key should create a new task")
	}
	if h.log.events[0].TaskID != first.Task.ID() || h.log.events[0].Payload["idempotencyKey"] != "alert-1" {
		t.Errorf("event = %+v", h.log.events[0])
	}
}

func TestSubmitTaskRefusals(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	uc := NewSubmitTask(h.deps)
	bad := []SubmitTaskCommand{
		{BotID: "", Objective: "o", IdempotencyKey: "k"},
		{BotID: "b", Objective: "", IdempotencyKey: "k"},
		{BotID: "b", Objective: "o", IdempotencyKey: ""},
		{BotID: "b", Objective: "o", IdempotencyKey: "k", CorrelationID: "has space"},
	}
	for i, c := range bad {
		if _, err := uc.Execute(ctx, c); err == nil {
			t.Errorf("bad command %d accepted", i)
		}
	}
	if _, err := uc.Execute(ctx, SubmitTaskCommand{BotID: "ghost", Objective: "o", IdempotencyKey: "k"}); !domain.IsNotFound(err) {
		t.Errorf("unknown bot: %v", err)
	}
	// Registered but not enabled: refused with a state error.
	bot, _ := NewRegisterBot(h.deps).Execute(ctx, sampleDefinition())
	_, err := uc.Execute(ctx, SubmitTaskCommand{BotID: bot.ID().String(), Objective: "o", IdempotencyKey: "k"})
	var serr domain.StateError
	if !errors.As(err, &serr) {
		t.Errorf("disabled bot: %v", err)
	}
	if last := h.log.audits[len(h.log.audits)-1]; last.Outcome != domain.AuditDenied {
		t.Errorf("denied audit missing: %+v", last)
	}
	// Port failures propagate.
	_, _ = NewEnableBot(h.deps).Execute(ctx, bot.ID())
	h.tasks.fail = errBoom
	if _, err := uc.Execute(ctx, SubmitTaskCommand{BotID: bot.ID().String(), Objective: "o", IdempotencyKey: "k"}); !errors.Is(err, errBoom) {
		t.Errorf("save error: %v", err)
	}
	h.tasks.fail = nil
	h.log.fail = errBoom
	if _, err := uc.Execute(ctx, SubmitTaskCommand{BotID: bot.ID().String(), Objective: "o", IdempotencyKey: "k"}); !errors.Is(err, errBoom) {
		t.Errorf("publish error: %v", err)
	}
	h.log.fail = nil
	first, _ := uc.Execute(ctx, SubmitTaskCommand{BotID: bot.ID().String(), Objective: "o", IdempotencyKey: "k2"})
	h.log.fail = errBoom
	if _, err := uc.Execute(ctx, SubmitTaskCommand{BotID: bot.ID().String(), Objective: "o", IdempotencyKey: "k2"}); !errors.Is(err, errBoom) || !first.Created {
		t.Errorf("dedup audit error: %v", err)
	}
}

func TestRunTaskCompletes(t *testing.T) {
	h := newHarness()
	bot := enabledBot(t, h)
	task := submit(t, h, bot, "alert-1").Task
	h.clock.now = t0.Add(time.Minute)

	res, err := NewRunTask(h.deps).Execute(context.Background(), task.ID())
	if err != nil {
		t.Fatal(err)
	}
	if res.Task.Status() != domain.TaskCompleted || res.Execution.Outcome() != domain.ExecutionCompleted {
		t.Errorf("task %s execution %s", res.Task.Status(), res.Execution.Outcome())
	}
	if res.Execution.Result().Answer != "done" || res.Execution.TracePath() != "/traces/exec-1.jsonl" || res.Task.CurrentExecutionID() != res.Execution.ID() {
		t.Errorf("execution = %+v", res.Execution.Snapshot())
	}
	if len(h.runtime.requests) != 1 {
		t.Fatalf("runtime called %d times", len(h.runtime.requests))
	}
	req := h.runtime.requests[0]
	if req.Objective != task.Objective() || req.Model != "anthropic/claude-haiku-5.5" || len(req.AllowedTools) != 2 || req.Timeout != 900*time.Second ||
		req.BotID != bot.ID() || req.TenantID != "engineering" || req.TaskID != task.ID() || req.ExecutionID != res.Execution.ID() || req.CorrelationID != "trace-abc" {
		t.Errorf("request = %+v", req)
	}
	if got := h.log.eventTypes(); len(got) != 3 || got[1] != "bot.task.started" || got[2] != "bot.task.completed" {
		t.Errorf("events = %v", got)
	}
	stored, _ := h.execs.Get(context.Background(), res.Execution.ID())
	if stored.Outcome() != domain.ExecutionCompleted {
		t.Error("execution not persisted as completed")
	}
	got, err := NewGetExecution(h.deps).Execute(context.Background(), res.Execution.ID())
	if err != nil || got.Result().Answer != "done" {
		t.Errorf("get execution: %v", err)
	}
	if _, err := NewGetExecution(h.deps).Execute(context.Background(), "nope"); !domain.IsNotFound(err) {
		t.Errorf("get missing execution: %v", err)
	}
	tk, err := NewGetTask(h.deps).Execute(context.Background(), task.ID())
	if err != nil || tk.Status() != domain.TaskCompleted {
		t.Errorf("get task: %v", err)
	}
	if _, err := NewGetTask(h.deps).Execute(context.Background(), "nope"); !domain.IsNotFound(err) {
		t.Errorf("get missing task: %v", err)
	}

	// A second run of the same task is refused and the runtime not called.
	if _, err := NewRunTask(h.deps).Execute(context.Background(), task.ID()); err == nil {
		t.Error("completed task ran again")
	}
	if len(h.runtime.requests) != 1 {
		t.Error("runtime called for a completed task")
	}
	if last := h.log.audits[len(h.log.audits)-1]; last.Outcome != domain.AuditDenied {
		t.Errorf("denied audit missing: %+v", last)
	}
}

func TestRunTaskOutcomes(t *testing.T) {
	cases := []struct {
		name       string
		report     ExecutionReport
		err        error
		taskStatus domain.TaskStatus
		outcome    domain.ExecutionOutcome
		event      string
		audit      domain.AuditOutcome
		reason     string
	}{
		{"failed", ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "exit 1"}, nil, domain.TaskFailed, domain.ExecutionFailed, "bot.task.failed", domain.AuditFailed, "exit 1"},
		{"failed without reason", ExecutionReport{Outcome: domain.ExecutionFailed}, nil, domain.TaskFailed, domain.ExecutionFailed, "bot.task.failed", domain.AuditFailed, "runtime reported Failed without a reason"},
		{"unknown outcome", ExecutionReport{Outcome: "Odd"}, nil, domain.TaskFailed, domain.ExecutionFailed, "bot.task.failed", domain.AuditFailed, "runtime reported Odd without a reason"},
		{"runtime error", ExecutionReport{}, errBoom, domain.TaskFailed, domain.ExecutionFailed, "bot.task.failed", domain.AuditFailed, "runtime: boom"},
		{"decision required", ExecutionReport{Outcome: domain.ExecutionDecisionRequired, Result: domain.Result{SessionID: "s", PendingDecision: &domain.PendingDecision{Tool: "local_exec", CallID: "c1", SessionID: "s"}}}, nil, domain.TaskWaitingApproval, domain.ExecutionDecisionRequired, "bot.task.waiting_approval", domain.AuditOK, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness()
			bot := enabledBot(t, h)
			task := submit(t, h, bot, "k").Task
			h.runtime.report, h.runtime.err = c.report, c.err
			res, err := NewRunTask(h.deps).Execute(context.Background(), task.ID())
			if err != nil {
				t.Fatal(err)
			}
			if res.Task.Status() != c.taskStatus || res.Execution.Outcome() != c.outcome || res.Execution.FailureReason() != c.reason {
				t.Errorf("task %s execution %s reason %q", res.Task.Status(), res.Execution.Outcome(), res.Execution.FailureReason())
			}
			if got := h.log.eventTypes(); got[len(got)-1] != c.event {
				t.Errorf("events = %v", got)
			}
			if last := h.log.audits[len(h.log.audits)-1]; last.Outcome != c.audit {
				t.Errorf("audit = %+v", last)
			}
		})
	}
}

func TestRunTaskRefusalsAndPortErrors(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	if _, err := NewRunTask(h.deps).Execute(ctx, "ghost"); !domain.IsNotFound(err) {
		t.Errorf("missing task: %v", err)
	}
	bot := enabledBot(t, h)
	task := submit(t, h, bot, "k").Task

	// Bot paused after submission: refused.
	paused := bot
	_ = paused.Pause(t0)
	_ = h.bots.Save(ctx, paused)
	if _, err := NewRunTask(h.deps).Execute(ctx, task.ID()); err == nil {
		t.Error("paused bot ran a task")
	}
	_ = h.bots.Save(ctx, bot)

	// Task whose bot vanished.
	orphan, _ := domain.NewTask("orphan", "ghost", "o", "k", "", t0)
	_ = h.tasks.Save(ctx, orphan)
	if _, err := NewRunTask(h.deps).Execute(ctx, "orphan"); !domain.IsNotFound(err) {
		t.Errorf("orphan: %v", err)
	}

	// Decision required without a pending decision is a runtime contract
	// violation: surfaced as an error.
	h.runtime.report = ExecutionReport{Outcome: domain.ExecutionDecisionRequired}
	if _, err := NewRunTask(h.deps).Execute(ctx, task.ID()); err == nil {
		t.Error("decision without pending call accepted")
	}

	for name, arrange := range map[string]func(*harness){
		"execution save": func(h *harness) { h.execs.fail = errBoom },
		"task save":      func(h *harness) { h.tasks.fail = errBoom },
		"publish":        func(h *harness) { h.log.fail = errBoom },
	} {
		h := newHarness()
		bot := enabledBot(t, h)
		task := submit(t, h, bot, "k").Task
		arrange(h)
		if _, err := NewRunTask(h.deps).Execute(ctx, task.ID()); !errors.Is(err, errBoom) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestRunTaskEndedNeverBeforeStarted(t *testing.T) {
	h := newHarness()
	bot := enabledBot(t, h)
	task := submit(t, h, bot, "k").Task
	// A clock that runs backwards must not produce an end before the start.
	h.clock.now = t0.Add(-time.Hour)
	res, err := NewRunTask(h.deps).Execute(context.Background(), task.ID())
	if err != nil {
		t.Fatal(err)
	}
	if res.Execution.EndedAt().Before(res.Execution.StartedAt()) {
		t.Error("ended before started")
	}
}
