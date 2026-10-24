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
		req.BotID != bot.ID() || req.TenantID != "engineering" || req.TaskID != task.ID() || req.ExecutionID != res.Execution.ID() || req.CorrelationID != "trace-abc" ||
		req.Level != domain.AutonomyObserve || !req.ToolRules.IsEmpty() {
		t.Errorf("request = %+v", req)
	}
	if got := h.store.reports[task.ID()]; got != "done" {
		t.Errorf("report artifact = %q", got)
	}
	if ref := ReportReference(res.Execution); ref != "/artifacts/"+task.ID().String()+"/"+res.Execution.ID().String()+"/report.md" {
		t.Errorf("report reference = %q", ref)
	}
	if res.Execution.Result().Artifacts[0].MediaType != ReportMediaType {
		t.Errorf("artifacts = %+v", res.Execution.Result().Artifacts)
	}
	if got := h.log.events[2].Payload["report"]; got != ReportReference(res.Execution) {
		t.Errorf("completed event payload = %+v", h.log.events[2].Payload)
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

// pendingReport is a runtime report that stops on a call a person must decide.
func pendingReport(session string) ExecutionReport {
	return ExecutionReport{Outcome: domain.ExecutionDecisionRequired, Result: domain.Result{Answer: "I will run it now.", SessionID: session, PendingDecision: &domain.PendingDecision{Tool: "local_exec", CallID: "c1", SessionID: session}}, TracePath: "/traces/exec-1.jsonl"}
}

// waitingTask runs a task until the runtime asks for a decision and clears
// the logs, so a test sees only what the decision itself records.
func waitingTask(t *testing.T, h *harness) (domain.Bot, domain.Task) {
	t.Helper()
	bot := enabledBot(t, h)
	task := submit(t, h, bot, "alert-1").Task
	h.runtime.report = pendingReport("sess-1")
	res, err := NewRunTask(h.deps).Execute(context.Background(), task.ID())
	if err != nil {
		t.Fatal(err)
	}
	if res.Task.Status() != domain.TaskWaitingApproval {
		t.Fatalf("task is %s", res.Task.Status())
	}
	h.log.events, h.log.audits = nil, nil
	return bot, res.Task
}

func TestApproveTaskCompletesUnderASecondExecution(t *testing.T) {
	h := newHarness()
	bot, task := waitingTask(t, h)
	h.clock.now = t0.Add(2 * time.Minute)
	ctx := context.Background()

	res, err := NewApproveTask(h.deps).Execute(ctx, task.ID(), DecisionApprove)
	if err != nil {
		t.Fatal(err)
	}
	if res.Task.Status() != domain.TaskCompleted || res.Execution.Outcome() != domain.ExecutionCompleted || res.Execution.Result().Answer != "done after approval" || res.Execution.TracePath() != "/traces/exec-2.jsonl" {
		t.Errorf("task %s execution %+v", res.Task.Status(), res.Execution.Snapshot())
	}
	ids := res.Task.ExecutionIDs()
	if len(ids) != 2 || ids[0] != task.CurrentExecutionID() || ids[1] != res.Execution.ID() || res.Execution.ID() == task.CurrentExecutionID() {
		t.Errorf("executions = %v (first %s, new %s)", ids, task.CurrentExecutionID(), res.Execution.ID())
	}
	if !res.Execution.StartedAt().Equal(h.clock.now) || res.Execution.BotID() != bot.ID() || res.Execution.TaskID() != task.ID() {
		t.Errorf("execution = %+v", res.Execution.Snapshot())
	}
	if len(h.decider.requests) != 1 || len(h.runtime.requests) != 1 {
		t.Fatalf("decider called %d times, runtime %d", len(h.decider.requests), len(h.runtime.requests))
	}
	req := h.decider.requests[0]
	if req.ExecutionID != res.Execution.ID() || req.BotID != bot.ID() || req.SessionID != "sess-1" || req.CallID != "c1" || req.Decision != "approve" || req.Timeout != 900*time.Second {
		t.Errorf("request = %+v", req)
	}
	if got := h.log.eventTypes(); len(got) != 2 || got[0] != "bot.task.decided" || got[1] != "bot.task.completed" {
		t.Errorf("events = %v", got)
	}
	decided := h.log.events[0]
	if decided.Payload["decision"] != "approve" || decided.Payload["callId"] != "c1" || decided.Payload["sessionId"] != "sess-1" || decided.Payload["executionId"] != res.Execution.ID().String() || decided.TaskID != task.ID() || decided.CorrelationID != "trace-abc" {
		t.Errorf("decided = %+v", decided)
	}
	if h.log.events[1].Payload["executionId"] != res.Execution.ID().String() {
		t.Errorf("completed = %+v", h.log.events[1])
	}
	if len(h.log.audits) != 1 || h.log.audits[0].Action != "task.decide" || h.log.audits[0].Outcome != domain.AuditOK || h.log.audits[0].Details["execution"] != res.Execution.ID().String() {
		t.Errorf("audits = %+v", h.log.audits)
	}
	stored, _ := h.execs.Get(ctx, res.Execution.ID())
	if stored.Outcome() != domain.ExecutionCompleted {
		t.Error("execution not persisted as completed")
	}
	first, _ := h.execs.Get(ctx, task.CurrentExecutionID())
	if first.Outcome() != domain.ExecutionDecisionRequired {
		t.Error("the first execution must keep its own outcome")
	}
	if tk, _ := h.tasks.Get(ctx, task.ID()); tk.Status() != domain.TaskCompleted || len(tk.ExecutionIDs()) != 2 {
		t.Errorf("stored task = %+v", tk.Snapshot())
	}

	// A decided task cannot be decided again; nothing reaches the runtime.
	if _, err := NewApproveTask(h.deps).Execute(ctx, task.ID(), DecisionApprove); err == nil {
		t.Error("completed task decided again")
	}
	if len(h.decider.requests) != 1 {
		t.Error("decider called for a completed task")
	}
	if last := h.log.audits[len(h.log.audits)-1]; last.Outcome != domain.AuditDenied || last.Action != "task.decide" {
		t.Errorf("denied audit missing: %+v", last)
	}
}

func TestApproveTaskOutcomes(t *testing.T) {
	cases := []struct {
		name       string
		decision   string
		report     ExecutionReport
		err        error
		taskStatus domain.TaskStatus
		outcome    domain.ExecutionOutcome
		event      string
		audit      domain.AuditOutcome
		reason     string
	}{
		{"deny completes", DecisionDeny, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "then I stop here"}}, nil, domain.TaskCompleted, domain.ExecutionCompleted, "bot.task.completed", domain.AuditOK, ""},
		{"failed", DecisionApprove, ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "exit 1"}, nil, domain.TaskFailed, domain.ExecutionFailed, "bot.task.failed", domain.AuditFailed, "exit 1"},
		{"executor error", DecisionApprove, ExecutionReport{}, errBoom, domain.TaskFailed, domain.ExecutionFailed, "bot.task.failed", domain.AuditFailed, "runtime: boom"},
		{"another decision", DecisionApprove, pendingReport("sess-1"), nil, domain.TaskWaitingApproval, domain.ExecutionDecisionRequired, "bot.task.waiting_approval", domain.AuditOK, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness()
			_, task := waitingTask(t, h)
			h.decider.report, h.decider.err = c.report, c.err
			res, err := NewApproveTask(h.deps).Execute(context.Background(), task.ID(), c.decision)
			if err != nil {
				t.Fatal(err)
			}
			if res.Task.Status() != c.taskStatus || res.Execution.Outcome() != c.outcome || res.Execution.FailureReason() != c.reason {
				t.Errorf("task %s execution %s reason %q", res.Task.Status(), res.Execution.Outcome(), res.Execution.FailureReason())
			}
			if h.decider.requests[0].Decision != c.decision || h.log.events[0].Payload["decision"] != c.decision {
				t.Errorf("decision %q not passed on: %+v", c.decision, h.decider.requests[0])
			}
			if got := h.log.eventTypes(); len(got) != 2 || got[1] != c.event {
				t.Errorf("events = %v", got)
			}
			if last := h.log.audits[len(h.log.audits)-1]; last.Outcome != c.audit {
				t.Errorf("audit = %+v", last)
			}
		})
	}
}

func TestApproveTaskDecidesAgainAfterAnotherStop(t *testing.T) {
	// Under --approve none the next held call stops the turn again: the task
	// goes back to WaitingApproval and a third execution carries the next
	// decision, on the new pending call.
	h := newHarness()
	_, task := waitingTask(t, h)
	second := pendingReport("sess-1")
	second.Result.PendingDecision.CallID = "c2"
	h.decider.report = second
	res, err := NewApproveTask(h.deps).Execute(context.Background(), task.ID(), DecisionApprove)
	if err != nil || res.Task.Status() != domain.TaskWaitingApproval {
		t.Fatalf("%v %s", err, res.Task.Status())
	}
	h.decider.report = ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done"}}
	res, err = NewApproveTask(h.deps).Execute(context.Background(), task.ID(), DecisionDeny)
	if err != nil || res.Task.Status() != domain.TaskCompleted || len(res.Task.ExecutionIDs()) != 3 {
		t.Fatalf("%v %s %v", err, res.Task.Status(), res.Task.ExecutionIDs())
	}
	if req := h.decider.requests[1]; req.CallID != "c2" || req.Decision != "deny" {
		t.Errorf("second request = %+v", req)
	}
}

func TestApproveTaskFallsBackToTheResultSession(t *testing.T) {
	h := newHarness()
	bot := enabledBot(t, h)
	task := submit(t, h, bot, "k").Task
	report := pendingReport("sess-9")
	report.Result.PendingDecision.SessionID = ""
	h.runtime.report = report
	if _, err := NewRunTask(h.deps).Execute(context.Background(), task.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := NewApproveTask(h.deps).Execute(context.Background(), task.ID(), DecisionApprove); err != nil {
		t.Fatal(err)
	}
	if h.decider.requests[0].SessionID != "sess-9" || h.log.events[len(h.log.events)-2].Payload["sessionId"] != "sess-9" {
		t.Errorf("session = %+v", h.decider.requests[0])
	}
}

func TestApproveTaskRefusals(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	uc := NewApproveTask(h.deps)
	bot, task := waitingTask(t, h)

	// refuse expects the decision to be refused with nothing changed: no
	// call to the runtime and no task saved.
	refuse := func(name string, id domain.TaskID, decision string, want func(error) bool) {
		t.Helper()
		saves := h.tasks.saves
		_, err := uc.Execute(ctx, id, decision)
		if err == nil || !want(err) {
			t.Errorf("%s: %v", name, err)
		}
		if len(h.decider.requests) != 0 || h.tasks.saves != saves {
			t.Errorf("%s: decider called %d times, task saved %d times", name, len(h.decider.requests), h.tasks.saves-saves)
		}
	}
	isState := func(err error) bool { var e domain.StateError; return errors.As(err, &e) }
	isValidation := func(err error) bool { var e domain.ValidationError; return errors.As(err, &e) }

	refuse("bad word", task.ID(), "maybe", isValidation)
	refuse("empty word", task.ID(), "", isValidation)
	refuse("missing task", "ghost", DecisionApprove, domain.IsNotFound)
	if len(h.log.audits) != 0 {
		t.Errorf("audits before any state refusal: %+v", h.log.audits)
	}

	pending := submit(t, h, bot, "other").Task
	refuse("pending task", pending.ID(), DecisionApprove, isState)
	if last := h.log.audits[len(h.log.audits)-1]; last.Outcome != domain.AuditDenied || last.Action != "task.decide" || last.Subject != pending.ID().String() {
		t.Errorf("denied audit = %+v", last)
	}

	paused := bot
	_ = paused.Pause(t0)
	_ = h.bots.Save(ctx, paused)
	refuse("paused bot", task.ID(), DecisionApprove, isState)
	_ = h.bots.Save(ctx, bot)

	orphan, _ := domain.NewTask("orphan", "ghost", "o", "k", "", t0)
	_ = orphan.Start("exec-orphan", t0)
	_ = orphan.AwaitApproval(t0)
	_ = h.tasks.Save(ctx, orphan)
	refuse("bot vanished", "orphan", DecisionApprove, domain.IsNotFound)

	lost, _ := domain.NewTask("lost", bot.ID(), "o", "k2", "", t0)
	_ = lost.Start("exec-lost", t0)
	_ = lost.AwaitApproval(t0)
	_ = h.tasks.Save(ctx, lost)
	refuse("execution vanished", "lost", DecisionApprove, domain.IsNotFound)

	// WaitingApproval but its execution holds no pending call: a corrupt
	// state, refused rather than sent to the runtime.
	execution, _ := domain.NewExecution("exec-lost", "lost", bot.ID(), t0)
	_ = execution.Complete(domain.Result{Answer: "x"}, domain.Usage{}, "", t0)
	_ = h.execs.Save(ctx, execution)
	refuse("no pending call", "lost", DecisionApprove, isState)
	if last := h.log.audits[len(h.log.audits)-1]; last.Outcome != domain.AuditDenied || last.Details["reason"] == "" {
		t.Errorf("denied audit = %+v", last)
	}
}

func TestApproveTaskPortErrors(t *testing.T) {
	ctx := context.Background()
	for name, arrange := range map[string]func(*harness){
		"execution save": func(h *harness) { h.execs.fail = errBoom },
		"task save":      func(h *harness) { h.tasks.fail = errBoom },
		"publish":        func(h *harness) { h.log.fail = errBoom },
	} {
		h := newHarness()
		_, task := waitingTask(t, h)
		arrange(h)
		if _, err := NewApproveTask(h.deps).Execute(ctx, task.ID(), DecisionApprove); !errors.Is(err, errBoom) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A decision the runtime answers with DecisionRequired but no pending
	// call breaks the contract: surfaced as an error, as in RunTask.
	h := newHarness()
	_, task := waitingTask(t, h)
	h.decider.report = ExecutionReport{Outcome: domain.ExecutionDecisionRequired}
	if _, err := NewApproveTask(h.deps).Execute(ctx, task.ID(), DecisionApprove); err == nil {
		t.Error("decision without pending call accepted")
	}
	// A clock that runs backwards must not end the execution before it began.
	h = newHarness()
	_, task = waitingTask(t, h)
	h.clock.now = t0.Add(-time.Hour)
	res, err := NewApproveTask(h.deps).Execute(ctx, task.ID(), DecisionApprove)
	if err != nil || res.Execution.EndedAt().Before(res.Execution.StartedAt()) {
		t.Errorf("%v ended %s started %s", err, res.Execution.EndedAt(), res.Execution.StartedAt())
	}
}

func TestDecisionRequestValidate(t *testing.T) {
	good := DecisionRequest{SessionID: "s", Decision: DecisionApprove}
	if err := good.Validate(); err != nil {
		t.Error(err)
	}
	for _, bad := range []DecisionRequest{{Decision: DecisionDeny}, {SessionID: "s", Decision: "maybe"}, {SessionID: "s"}} {
		if err := bad.Validate(); err == nil {
			t.Errorf("%+v validated", bad)
		}
	}
	if !IsDecision("approve") || !IsDecision("deny") || IsDecision("Approve") || IsDecision("") {
		t.Error("IsDecision wrong")
	}
}
