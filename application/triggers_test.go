package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

const webhookSecret = "whsec-0123456789abcdef"

// triggeredBot registers and enables a bot with a manual, a webhook and
// a timer trigger.
func triggeredBot(t *testing.T, h *harness, triggers ...domain.TriggerSnapshot) domain.Bot {
	t.Helper()
	ctx := context.Background()
	definition := sampleDefinition()
	if len(triggers) == 0 {
		triggers = []domain.TriggerSnapshot{
			{Type: "manual"},
			{Type: "webhook", Name: "deploy", Objective: "Handle event {{.type}} for {{.service}}", Secret: webhookSecret},
			{Type: "timer", Name: "tick", Objective: "say the time ({{.scheduledAt}})", Every: "10m"},
		}
	}
	definition.Triggers = triggers
	bot, err := NewRegisterBot(h.deps).Execute(ctx, definition)
	if err != nil {
		t.Fatal(err)
	}
	if bot, err = NewEnableBot(h.deps).Execute(ctx, bot.ID()); err != nil {
		t.Fatal(err)
	}
	h.log.events, h.log.audits = nil, nil
	return bot
}

func TestWebhookFireIsIdempotentOnRedelivery(t *testing.T) {
	h := newHarness()
	bot := triggeredBot(t, h)
	ctx := context.Background()
	uc := NewFireTrigger(h.deps)
	body := []byte(`{"type": "deploy", "service": "web"}`)
	first, err := uc.Execute(ctx, FireTriggerCommand{BotID: bot.ID().String(), TriggerName: "deploy", DeliveryID: "gh-1", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created || first.Task.Objective() != "Handle event deploy for web" || first.Task.Trigger().Name != "deploy" || string(first.Task.Input()) != `{"type":"deploy","service":"web"}` {
		t.Errorf("first = %+v", first.Task.Snapshot())
	}
	if !strings.HasPrefix(first.IdempotencyKey.String(), "webhook:deploy:") || first.Task.IdempotencyKey() != first.IdempotencyKey {
		t.Errorf("key = %s", first.IdempotencyKey)
	}
	if !strings.HasPrefix(first.Task.CorrelationID().String(), "trace-") {
		t.Errorf("no trace id minted: %q", first.Task.CorrelationID())
	}
	// The same delivery again, even with a changed body, is the same task.
	again, err := uc.Execute(ctx, FireTriggerCommand{BotID: bot.ID().String(), TriggerName: "deploy", DeliveryID: "gh-1", Body: []byte(`{"type": "deploy", "service": "api"}`)})
	if err != nil || again.Created || again.Task.ID() != first.Task.ID() {
		t.Errorf("redelivery: %v created %v task %s vs %s", err, again.Created, again.Task.ID(), first.Task.ID())
	}
	if h.tasks.saves != 1 {
		t.Errorf("task saved %d times", h.tasks.saves)
	}
	types := h.log.eventTypes()
	if strings.Join(types, " ") != "bot.task.submitted bot.trigger.fired bot.trigger.fired" {
		t.Errorf("events = %v", types)
	}
	fired := h.log.events[2]
	if fired.Payload["deduplicated"] != true || fired.Payload["created"] != false || fired.Payload["taskId"] != first.Task.ID().String() || fired.Payload["deliveryId"] != "gh-1" || fired.TaskID != first.Task.ID() || fired.CorrelationID != first.Task.CorrelationID() {
		t.Errorf("fired event = %+v", fired)
	}
	if h.log.events[0].Payload["trigger"].(map[string]any)["name"] != "deploy" {
		t.Errorf("submitted event = %+v", h.log.events[0])
	}
	// Without a delivery id the body decides: the same body is the same task, another body another task.
	byBody, _ := uc.Execute(ctx, FireTriggerCommand{BotID: bot.ID().String(), TriggerName: "deploy", Body: body})
	sameBody, _ := uc.Execute(ctx, FireTriggerCommand{BotID: bot.ID().String(), TriggerName: "deploy", Body: body})
	otherBody, _ := uc.Execute(ctx, FireTriggerCommand{BotID: bot.ID().String(), TriggerName: "deploy", Body: []byte(`{"type": "deploy", "service": "api"}`)})
	if !byBody.Created || sameBody.Created || sameBody.Task.ID() != byBody.Task.ID() || !otherBody.Created || otherBody.Task.ID() == byBody.Task.ID() {
		t.Error("body hash dedup wrong")
	}
}

func TestFireTriggerRefusals(t *testing.T) {
	h := newHarness()
	bot := triggeredBot(t, h)
	ctx := context.Background()
	uc := NewFireTrigger(h.deps)
	body := []byte(`{"type": "deploy", "service": "web"}`)
	if _, err := uc.Execute(ctx, FireTriggerCommand{BotID: "Bad", TriggerName: "deploy", Body: body}); err == nil {
		t.Error("bad bot id accepted")
	}
	if _, err := uc.Execute(ctx, FireTriggerCommand{BotID: "ghost", TriggerName: "deploy", Body: body}); !domain.IsNotFound(err) {
		t.Error("unknown bot:", err)
	}
	if _, err := uc.Execute(ctx, FireTriggerCommand{BotID: bot.ID().String(), TriggerName: "nope", Body: body}); !domain.IsNotFound(err) {
		t.Error("unknown trigger:", err)
	}
	var verr domain.ValidationError
	if _, err := uc.Execute(ctx, FireTriggerCommand{BotID: bot.ID().String(), TriggerName: "manual", Body: body}); !errors.As(err, &verr) {
		t.Error("manual trigger fired:", err)
	}
	if _, err := uc.Execute(ctx, FireTriggerCommand{BotID: bot.ID().String(), TriggerName: "deploy", Body: []byte(`[1]`)}); !errors.As(err, &verr) {
		t.Error("array body accepted:", err)
	}
	if _, err := uc.Execute(ctx, FireTriggerCommand{BotID: bot.ID().String(), TriggerName: "deploy", Body: []byte(`{"type": "deploy"}`)}); !errors.As(err, &verr) {
		t.Error("body without the template's field accepted:", err)
	}
	if _, err := uc.Execute(ctx, FireTriggerCommand{BotID: bot.ID().String(), TriggerName: "tick"}); !errors.As(err, &verr) {
		t.Error("timer fire without a slot accepted:", err)
	}
	if len(h.tasks.tasks) != 0 {
		t.Error("a refused fire created a task")
	}
	// A bot that takes no tasks refuses the fire (and the event is not published).
	paused, _ := NewRegisterBot(h.deps).Execute(ctx, func() BotDefinition {
		d := sampleDefinition()
		d.Name = "paused"
		d.Triggers = []domain.TriggerSnapshot{{Type: "webhook", Name: "w", Objective: "x", Secret: webhookSecret}}
		return d
	}())
	var serr domain.StateError
	if _, err := uc.Execute(ctx, FireTriggerCommand{BotID: paused.ID().String(), TriggerName: "w", Body: body}); !errors.As(err, &serr) {
		t.Error("registered bot accepted a fire:", err)
	}
	h.log.fail = errBoom
	if _, err := uc.Execute(ctx, FireTriggerCommand{BotID: bot.ID().String(), TriggerName: "deploy", Body: body}); !errors.Is(err, errBoom) {
		t.Error("publish error:", err)
	}
	h.log.fail = nil
	ok, _ := uc.Execute(ctx, FireTriggerCommand{BotID: bot.ID().String(), TriggerName: "deploy", DeliveryID: "fresh", Body: body})
	h.log.fail = errBoom
	if _, err := uc.Execute(ctx, FireTriggerCommand{BotID: bot.ID().String(), TriggerName: "deploy", DeliveryID: "fresh", Body: body}); !errors.Is(err, errBoom) || !ok.Created {
		t.Error("dedup publish error:", err)
	}
}

func TestTimersArmFireAndCatchUpOnce(t *testing.T) {
	h := newHarness()
	bot := triggeredBot(t, h,
		domain.TriggerSnapshot{Type: "manual"},
		domain.TriggerSnapshot{Type: "timer", Name: "tick", Objective: "say the time at {{.scheduledAt}}", Every: "10m"},
		domain.TriggerSnapshot{Type: "timer", Name: "nightly", Objective: "report", Cron: "0 3 * * *"},
	)
	ctx := context.Background()
	uc := NewTickTimers(h.deps)

	// First pass: the interval fires at once, the cron waits for 03:00.
	result, err := uc.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Fired) != 1 || result.Fired[0].Trigger != "tick" || !result.Fired[0].Created || !result.Fired[0].ScheduledAt.Equal(t0) || len(result.Errors) != 0 {
		t.Fatalf("first pass = %+v", result)
	}
	task, _ := h.tasks.Get(ctx, result.Fired[0].TaskID)
	if task.Objective() != "say the time at 2026-10-23T15:00:00Z" || task.Trigger().Type != domain.TriggerTimer || task.IdempotencyKey() != domain.FireKeyTimer("tick", t0) {
		t.Errorf("timer task = %+v", task.Snapshot())
	}
	tick, _ := h.timers.Get(ctx, domain.TimerKey(bot.ID(), "tick"))
	nightly, _ := h.timers.Get(ctx, domain.TimerKey(bot.ID(), "nightly"))
	if !tick.NextFireAt().Equal(t0.Add(10*time.Minute)) || tick.LastTaskID() != task.ID() || !nightly.NextFireAt().Equal(time.Date(2026, 10, 24, 3, 0, 0, 0, time.UTC)) {
		t.Errorf("timers = %+v / %+v", tick.Snapshot(), nightly.Snapshot())
	}
	// Second pass a minute later: nothing is due.
	h.clock.now = t0.Add(time.Minute)
	if result, _ := uc.Execute(ctx); len(result.Fired) != 0 {
		t.Errorf("fired early: %+v", result.Fired)
	}
	// The daemon is down for an hour: six slots missed, one catch-up fire
	// for the persisted slot, and the next fire is after now.
	h.clock.now = t0.Add(time.Hour)
	result, _ = uc.Execute(ctx)
	if len(result.Fired) != 1 || !result.Fired[0].ScheduledAt.Equal(t0.Add(10*time.Minute)) || !result.Fired[0].Created {
		t.Fatalf("catch-up = %+v", result.Fired)
	}
	tick, _ = h.timers.Get(ctx, domain.TimerKey(bot.ID(), "tick"))
	if !tick.NextFireAt().Equal(t0.Add(70 * time.Minute)) {
		t.Errorf("next after catch-up = %s", tick.NextFireAt())
	}
	// A crash between the fire and the save: the same slot fires again and finds the same task.
	stale := tick
	_ = stale.Rearm(stale.ScheduleKey(), t0.Add(10*time.Minute))
	_ = h.timers.Save(ctx, stale)
	result, _ = uc.Execute(ctx)
	if len(result.Fired) != 1 || result.Fired[0].Created || result.Fired[0].TaskID != tick.LastTaskID() {
		t.Errorf("replayed slot = %+v", result.Fired)
	}
	if n := countTasks(h, domain.TriggerTimer); n != 2 {
		t.Errorf("%d timer tasks, want 2", n)
	}
	// The cron fires at its slot, once, with that slot's key.
	h.clock.now = time.Date(2026, 10, 24, 3, 0, 30, 0, time.UTC)
	result, _ = uc.Execute(ctx)
	if len(result.Fired) != 2 {
		t.Fatalf("at 03:00: %+v", result.Fired)
	}
	nightly, _ = h.timers.Get(ctx, domain.TimerKey(bot.ID(), "nightly"))
	if !nightly.NextFireAt().Equal(time.Date(2026, 10, 25, 3, 0, 0, 0, time.UTC)) {
		t.Errorf("nightly next = %s", nightly.NextFireAt())
	}
}

func countTasks(h *harness, kind domain.TriggerType) int {
	n := 0
	for _, task := range h.tasks.tasks {
		if task.Trigger().Type == kind {
			n++
		}
	}
	return n
}

func TestTimersFollowTheBotsDefinitionAndStatus(t *testing.T) {
	h := newHarness()
	bot := triggeredBot(t, h, domain.TriggerSnapshot{Type: "timer", Name: "tick", Objective: "x", Every: "10m"})
	ctx := context.Background()
	uc := NewTickTimers(h.deps)
	if result, _ := uc.Execute(ctx); len(result.Fired) != 1 {
		t.Fatal("first fire missing")
	}
	// A paused bot: due timers are advanced, not fired, and no backlog builds up.
	paused, _ := h.bots.Get(ctx, bot.ID())
	_ = paused.Pause(h.clock.now)
	_ = h.bots.Save(ctx, paused)
	h.clock.now = t0.Add(time.Hour)
	if result, _ := uc.Execute(ctx); len(result.Fired) != 0 {
		t.Error("paused bot fired")
	}
	timer, _ := h.timers.Get(ctx, domain.TimerKey(bot.ID(), "tick"))
	if !timer.NextFireAt().Equal(t0.Add(70 * time.Minute)) {
		t.Errorf("paused timer not advanced: %s", timer.NextFireAt())
	}
	// Enabled again before the next slot: nothing fires.
	_, _ = NewEnableBot(h.deps).Execute(ctx, bot.ID())
	if result, _ := uc.Execute(ctx); len(result.Fired) != 0 {
		t.Error("re-enabled bot replayed the backlog")
	}
	// A changed schedule re-arms the timer (an interval: at once).
	every := "5m"
	if _, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Triggers: &[]domain.TriggerSnapshot{{Type: "timer", Name: "tick", Objective: "x", Every: every}}}); err != nil {
		t.Fatal(err)
	}
	h.clock.now = t0.Add(71 * time.Minute)
	result, _ := uc.Execute(ctx)
	timer, _ = h.timers.Get(ctx, domain.TimerKey(bot.ID(), "tick"))
	if timer.ScheduleKey() != "every 5m0s" || len(result.Fired) != 0 || !timer.NextFireAt().Equal(t0.Add(76*time.Minute)) {
		t.Errorf("re-armed = %+v fired %+v", timer.Snapshot(), result.Fired)
	}
	// A removed trigger drops its timer.
	if _, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Triggers: &[]domain.TriggerSnapshot{{Type: "manual"}}}); err != nil {
		t.Fatal(err)
	}
	if _, _ = uc.Execute(ctx); len(h.timers.timers) != 0 {
		t.Errorf("orphan timer kept: %v", h.timers.timers)
	}
	// Errors of one timer are reported and do not stop the pass.
	_, _ = NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Triggers: &[]domain.TriggerSnapshot{{Type: "timer", Name: "tick", Objective: "x {{.missing}}", Every: "1m"}}})
	result, err := uc.Execute(ctx)
	if err != nil || len(result.Errors) != 1 || result.Errors[0].Trigger != "tick" {
		t.Errorf("template error not reported: %v %+v", err, result)
	}
	h.timers.fail = errBoom
	if _, err := uc.Execute(ctx); !errors.Is(err, errBoom) {
		t.Error("timer list error:", err)
	}
	h.timers.fail = nil
	h.bots.fail = nil
}

func TestUpdateBot(t *testing.T) {
	h := newHarness()
	bot := triggeredBot(t, h)
	ctx := context.Background()
	uc := NewUpdateBot(h.deps)
	mission, level := "Keep the hosts healthy", "propose"
	tools := []string{"observability.query_logs", "local_exec"}
	rules := domain.ToolRulesSnapshot{Allow: []domain.ToolRuleSnapshot{{Tool: "local_exec", Arguments: map[string]string{"program": "ls"}}}}
	h.clock.now = t0.Add(time.Hour)
	updated, err := uc.Execute(ctx, bot.ID(), BotPatch{Mission: &mission, Level: &level, AllowedTools: &tools, ToolRules: &rules, Budgets: &BudgetsDefinition{TaskTimeoutSeconds: 30}, Model: &ModelPolicyDefinition{Model: "m2"}, Placement: &domain.PlacementSnapshot{Nodes: []string{"web-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Mission().String() != mission || updated.Level() != domain.AutonomyPropose || len(updated.AllowedTools().Names()) != 2 || len(updated.ToolRules().Allow()) != 1 || updated.Budgets().TaskTimeout() != 30*time.Second || updated.ModelPolicy().Model() != "m2" || updated.Placement().IsEmpty() || !updated.UpdatedAt().Equal(t0.Add(time.Hour)) {
		t.Errorf("updated = %+v", updated.Snapshot())
	}
	if len(updated.Triggers().List()) != 3 || updated.Status() != domain.BotEnabled {
		t.Error("triggers or status changed by a patch that named neither")
	}
	if got := h.log.eventTypes(); len(got) != 1 || got[0] != "bot.updated" || len(h.log.events[0].Payload["fields"].([]string)) != 7 {
		t.Errorf("events = %v %+v", got, h.log.events)
	}
	// A narrower tool list that strands a rule changes nothing.
	narrower := []string{"observability.query_logs"}
	if _, err := uc.Execute(ctx, bot.ID(), BotPatch{AllowedTools: &narrower}); err == nil {
		t.Error("stranded rule accepted")
	}
	still, _ := h.bots.Get(ctx, bot.ID())
	if len(still.AllowedTools().Names()) != 2 {
		t.Error("a refused patch changed the bot")
	}
	// Replacing the rules along with the tools is fine.
	none := domain.ToolRulesSnapshot{}
	if _, err := uc.Execute(ctx, bot.ID(), BotPatch{AllowedTools: &narrower, ToolRules: &none}); err != nil {
		t.Error("tools and rules together refused:", err)
	}
	var verr domain.ValidationError
	if _, err := uc.Execute(ctx, bot.ID(), BotPatch{}); !errors.As(err, &verr) {
		t.Error("empty patch accepted:", err)
	}
	bad := "reckless"
	if _, err := uc.Execute(ctx, bot.ID(), BotPatch{Level: &bad}); err == nil {
		t.Error("bad level accepted")
	}
	badTriggers := []domain.TriggerSnapshot{{Type: "webhook", Objective: "x"}}
	if _, err := uc.Execute(ctx, bot.ID(), BotPatch{Triggers: &badTriggers}); err == nil {
		t.Error("bad trigger accepted")
	}
	badMission, badModel, badTools, badPlacement, badBudgets, badRules := "", ModelPolicyDefinition{RoutingPolicy: "fastest"}, []string{"Bad Tool"}, domain.PlacementSnapshot{Nodes: []string{"Bad"}}, BudgetsDefinition{TaskTimeoutSeconds: -1}, domain.ToolRulesSnapshot{Allow: []domain.ToolRuleSnapshot{{Tool: "ghost"}}}
	for name, patch := range map[string]BotPatch{
		"mission": {Mission: &badMission}, "model": {Model: &badModel}, "tools": {AllowedTools: &badTools},
		"placement": {Placement: &badPlacement}, "budgets": {Budgets: &badBudgets}, "rules": {ToolRules: &badRules},
	} {
		if _, err := uc.Execute(ctx, bot.ID(), patch); err == nil {
			t.Errorf("bad %s accepted", name)
		}
	}
	if _, err := uc.Execute(ctx, "ghost", BotPatch{Mission: &mission}); !domain.IsNotFound(err) {
		t.Error("unknown bot:", err)
	}
	h.bots.fail = errBoom
	if _, err := uc.Execute(ctx, bot.ID(), BotPatch{Mission: &mission}); !errors.Is(err, errBoom) {
		t.Error("save error:", err)
	}
	h.bots.fail = nil
	h.log.fail = errBoom
	if _, err := uc.Execute(ctx, bot.ID(), BotPatch{Mission: &mission}); !errors.Is(err, errBoom) {
		t.Error("publish error:", err)
	}
}

// blockingRuntime holds a run until its context ends or it is released.
type blockingRuntime struct {
	mu      sync.Mutex
	started chan struct{}
	release chan struct{}
	request ExecutionRequest
}

func (b *blockingRuntime) Execute(ctx context.Context, request ExecutionRequest) (ExecutionReport, error) {
	b.mu.Lock()
	b.request = request
	b.mu.Unlock()
	close(b.started)
	select {
	case <-ctx.Done():
		return ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "interrupted: " + ctx.Err().Error()}, nil
	case <-b.release:
		return ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done"}}, nil
	}
}

func TestCancelTask(t *testing.T) {
	h := newHarness()
	bot := enabledBot(t, h)
	ctx := context.Background()
	cancel := NewCancelTask(h.deps)

	// Pending: the task is Cancelled, no execution exists.
	pending := submit(t, h, bot, "p").Task
	result, err := cancel.Execute(ctx, pending.ID())
	if err != nil || !result.Changed || result.Execution != nil || result.Task.Status() != domain.TaskCancelled {
		t.Fatalf("pending cancel: %v %+v", err, result)
	}
	again, err := cancel.Execute(ctx, pending.ID())
	if err != nil || again.Changed || again.Task.Status() != domain.TaskCancelled {
		t.Errorf("second cancel: %v %+v", err, again)
	}
	if got := h.log.eventTypes(); strings.Join(got, " ") != "bot.task.submitted bot.task.cancelled" {
		t.Errorf("events = %v", got)
	}
	// Completed: refused.
	done := submit(t, h, bot, "d").Task
	if _, err := NewRunTask(h.deps).Execute(ctx, done.ID()); err != nil {
		t.Fatal(err)
	}
	var serr domain.StateError
	if _, err := cancel.Execute(ctx, done.ID()); !errors.As(err, &serr) {
		t.Error("completed task cancelled:", err)
	}
	if _, err := cancel.Execute(ctx, "ghost"); !domain.IsNotFound(err) {
		t.Error("unknown task:", err)
	}
	// Running in process: the execution is Cancelled, the run is interrupted
	// and its late report is dropped.
	running := submit(t, h, bot, "r").Task
	blocker := &blockingRuntime{started: make(chan struct{}), release: make(chan struct{})}
	h.deps.Runtime = blocker
	runner := NewSerialRunner(h.deps)
	type outcome struct {
		result RunTaskResult
		err    error
	}
	ran := make(chan outcome, 1)
	go func() {
		res, err := runner.Run(ctx, running.ID())
		ran <- outcome{res, err}
	}()
	<-blocker.started
	if runner.Interrupt("other") {
		t.Error("interrupted a task that is not running")
	}
	cancelled, err := NewCancelTask(h.deps).Execute(ctx, running.ID())
	if err != nil || cancelled.Execution == nil || cancelled.Execution.Outcome() != domain.ExecutionCancelled || cancelled.Task.Status() != domain.TaskCancelled {
		t.Fatalf("running cancel: %v %+v", err, cancelled)
	}
	if !runner.Interrupt(running.ID()) {
		t.Error("the run was not in flight")
	}
	select {
	case out := <-ran:
		if out.err != nil || out.result.Task.Status() != domain.TaskCancelled || out.result.Execution.Outcome() != domain.ExecutionCancelled {
			t.Errorf("interrupted run settled as %v %s/%s", out.err, out.result.Task.Status(), out.result.Execution.Outcome())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run did not stop")
	}
	stored, _ := h.execs.Get(ctx, cancelled.Execution.ID())
	if stored.Outcome() != domain.ExecutionCancelled || stored.FailureReason() != "cancelled by test:operator" {
		t.Errorf("stored execution = %+v", stored.Snapshot())
	}
	last := h.log.audits[len(h.log.audits)-1]
	if last.Details["dropped"] == "" {
		t.Errorf("the late report was not audited as dropped: %+v", last)
	}
	for _, e := range h.log.events {
		if e.Type == domain.EventTaskCancelled && e.TaskID == running.ID() && (e.ExecutionID != cancelled.Execution.ID() || e.Payload["executionId"] != cancelled.Execution.ID().String()) {
			t.Errorf("cancelled event = %+v", e)
		}
	}
	// WaitingApproval: the task is Cancelled, the ended execution untouched.
	h.deps.Runtime = h.runtime
	h.runtime.report = ExecutionReport{Outcome: domain.ExecutionDecisionRequired, Result: domain.Result{SessionID: "s", PendingDecision: &domain.PendingDecision{Tool: "local_exec", CallID: "c1"}}}
	waiting := submit(t, h, bot, "w").Task
	if _, err := NewRunTask(h.deps).Execute(ctx, waiting.ID()); err != nil {
		t.Fatal(err)
	}
	result, err = cancel.Execute(ctx, waiting.ID())
	if err != nil || result.Execution != nil || result.Task.Status() != domain.TaskCancelled {
		t.Errorf("waiting cancel: %v %+v", err, result)
	}
	// A node-held execution: cancelled too, with the node in the event.
	node := onlineNode(t, h, "web-1")
	assigned := submit(t, h, bot, "n").Task
	if _, found, err := NewNextAssignment(h.deps).Execute(ctx, node.ID()); err != nil || !found {
		t.Fatal("assignment missing", err)
	}
	result, err = cancel.Execute(ctx, assigned.ID())
	if err != nil || result.Execution == nil || !result.Execution.IsAssigned() {
		t.Fatalf("assigned cancel: %v %+v", err, result)
	}
	if _, err := NewRenewLease(h.deps).Execute(ctx, node.ID(), result.Execution.ID()); err == nil {
		t.Error("lease renewed on a cancelled execution")
	}
	report, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), result.Execution.ID(), ExecutionReport{Outcome: domain.ExecutionCompleted})
	if err != nil || report.Accepted {
		t.Error("the node's late report was accepted:", err)
	}
	// Port errors.
	erroring := submit(t, h, bot, "e").Task
	h.tasks.fail = errBoom
	if _, err := cancel.Execute(ctx, erroring.ID()); err == nil {
		t.Error("save error swallowed")
	}
	h.tasks.fail = nil
}

func TestCorrelationOnExecutionsAndEvents(t *testing.T) {
	h := newHarness()
	bot := enabledBot(t, h)
	ctx := context.Background()
	noTrace, _ := NewSubmitTask(h.deps).Execute(ctx, SubmitTaskCommand{BotID: bot.ID().String(), Objective: "o", IdempotencyKey: "k"})
	if !strings.HasPrefix(noTrace.Task.CorrelationID().String(), "trace-") {
		t.Errorf("no trace id minted: %q", noTrace.Task.CorrelationID())
	}
	h.runtime.report.Correlation = &domain.CorrelationRecord{Carried: true, Path: "/correlation/exec.json"}
	res, err := NewRunTask(h.deps).Execute(ctx, noTrace.Task.ID())
	if err != nil {
		t.Fatal(err)
	}
	c := res.Execution.Correlation()
	if c.TraceID != noTrace.Task.CorrelationID().String() || c.ExecutionID != res.Execution.ID().String() || c.TaskID != noTrace.Task.ID().String() || c.BotID != bot.ID().String() || !c.Carried || c.Path != "/correlation/exec.json" {
		t.Errorf("correlation = %+v", c)
	}
	for _, e := range h.log.events {
		if e.Type == domain.EventTaskStarted || e.Type == domain.EventTaskCompleted {
			if e.ExecutionID != res.Execution.ID() || e.CorrelationID != noTrace.Task.CorrelationID() || e.TaskID != noTrace.Task.ID() {
				t.Errorf("event %s lacks the ids: %+v", e.Type, e)
			}
		}
	}
	executions, err := NewListExecutions(h.deps).Execute(ctx, noTrace.Task.ID())
	if err != nil || len(executions) != 1 || executions[0].ID() != res.Execution.ID() {
		t.Errorf("executions = %v %v", executions, err)
	}
	if _, err := NewListExecutions(h.deps).Execute(ctx, "ghost"); !domain.IsNotFound(err) {
		t.Error("unknown task:", err)
	}
	broken := noTrace.Task
	_ = broken.Start("exec-missing", t0)
	_ = h.tasks.Save(ctx, broken)
	if _, err := NewListExecutions(h.deps).Execute(ctx, broken.ID()); err == nil {
		t.Error("missing execution not reported")
	}
	events, err := NewListEvents(h.deps).Execute(ctx, bot.ID(), 2)
	if err != nil || len(events) != 2 || events[1].Type != domain.EventTaskCompleted {
		t.Errorf("events = %v %v", events, err)
	}
	if all, _ := NewListEvents(h.deps).Execute(ctx, bot.ID(), 0); len(all) != 3 {
		t.Errorf("default limit: %d events", len(all))
	}
	if all, _ := NewListEvents(h.deps).Execute(ctx, bot.ID(), MaxEventLimit+1); len(all) != 3 {
		t.Errorf("max limit: %d events", len(all))
	}
	if _, err := NewListEvents(h.deps).Execute(ctx, "ghost", 1); !domain.IsNotFound(err) {
		t.Error("unknown bot:", err)
	}
	h.log.fail = errBoom
	if _, err := NewListEvents(h.deps).Execute(ctx, bot.ID(), 1); !errors.Is(err, errBoom) {
		t.Error("reader error:", err)
	}
	h.log.fail = nil
	h.log.events = nil
	if empty, err := NewListEvents(h.deps).Execute(ctx, bot.ID(), 1); err != nil || empty == nil || len(empty) != 0 {
		t.Error("no events should be an empty list")
	}
}
