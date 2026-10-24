package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/underpass-bots/domain"
)

// governedBot registers and enables a bot at the level with a rule on
// local_exec, as the SRE bot is defined.
func governedBot(t *testing.T, h *harness, level string) domain.Bot {
	t.Helper()
	d := sampleDefinition()
	d.Name = "sre-host-bot"
	d.AllowedTools = []string{"local_read", "local_exec"}
	d.Level = level
	d.ToolRules = domain.ToolRulesSnapshot{Allow: []domain.ToolRuleSnapshot{{Tool: "local_exec", Arguments: map[string]string{"program": "journalctl"}}}}
	bot, err := NewRegisterBot(h.deps).Execute(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	bot, err = NewEnableBot(h.deps).Execute(context.Background(), bot.ID())
	if err != nil {
		t.Fatal(err)
	}
	return bot
}

func TestRunTaskHandsThePolicyToTheRuntimeAndRecordsIt(t *testing.T) {
	h := newHarness()
	bot := governedBot(t, h, "act-with-approval")
	task := submit(t, h, bot, "alert-1").Task
	record := domain.PolicyRecord{Level: "act-with-approval", RuntimeLevel: "act-with-approval", Enforced: true, Path: "/state/policies/exec-2.json"}
	h.runtime.report.Policy = &record

	res, err := NewRunTask(h.deps).Execute(context.Background(), task.ID())
	if err != nil {
		t.Fatal(err)
	}
	req := h.runtime.requests[0]
	if req.Level != domain.AutonomyActWithApproval || !req.ToolRules.Constrains("local_exec") || len(req.AllowedTools) != 2 {
		t.Errorf("request = %+v", req)
	}
	if res.Execution.Policy() != record {
		t.Errorf("policy = %+v", res.Execution.Policy())
	}
	stored, _ := h.execs.Get(context.Background(), res.Execution.ID())
	if stored.Policy() != record {
		t.Error("policy not persisted")
	}

	// A runtime error keeps the policy the adapter reported with it.
	h = newHarness()
	bot = governedBot(t, h, "observe")
	task = submit(t, h, bot, "alert-2").Task
	h.runtime.err = errBoom
	h.runtime.report = ExecutionReport{Policy: &domain.PolicyRecord{Level: "observe", Enforced: false}}
	res, err = NewRunTask(h.deps).Execute(context.Background(), task.ID())
	if err != nil || res.Execution.Outcome() != domain.ExecutionFailed || res.Execution.Policy().Level != "observe" {
		t.Errorf("failed run: %v %+v", err, res.Execution.Snapshot())
	}

	// A policy record without a level is refused before anything is saved.
	h = newHarness()
	bot = governedBot(t, h, "observe")
	task = submit(t, h, bot, "alert-3").Task
	h.runtime.report.Policy = &domain.PolicyRecord{}
	if _, err := NewRunTask(h.deps).Execute(context.Background(), task.ID()); err == nil {
		t.Error("empty policy record accepted")
	}
}

func TestRunTaskReportArtifactFailureEndsTheRun(t *testing.T) {
	h := newHarness()
	bot := enabledBot(t, h)
	task := submit(t, h, bot, "alert-1").Task
	h.store.fail = errBoom
	if _, err := NewRunTask(h.deps).Execute(context.Background(), task.ID()); !errors.Is(err, errBoom) {
		t.Errorf("artifact error: %v", err)
	}
	// A failed run or one without an answer leaves no report.
	h = newHarness()
	bot = enabledBot(t, h)
	task = submit(t, h, bot, "alert-2").Task
	h.runtime.report = ExecutionReport{Outcome: domain.ExecutionCompleted}
	res, err := NewRunTask(h.deps).Execute(context.Background(), task.ID())
	if err != nil || len(res.Execution.Result().Artifacts) != 0 || ReportReference(res.Execution) != "" {
		t.Errorf("empty answer: %v %+v", err, res.Execution.Result())
	}
}

func TestApproveTaskCarriesThePolicy(t *testing.T) {
	h := newHarness()
	bot := governedBot(t, h, "act-with-approval")
	task := submit(t, h, bot, "alert-1").Task
	h.runtime.report = ExecutionReport{Outcome: domain.ExecutionDecisionRequired, Result: domain.Result{SessionID: "sess-1", PendingDecision: &domain.PendingDecision{Tool: "local_exec", CallID: "c1", SessionID: "sess-1"}}}
	if _, err := NewRunTask(h.deps).Execute(context.Background(), task.ID()); err != nil {
		t.Fatal(err)
	}
	h.decider.report.Policy = &domain.PolicyRecord{Level: "act-with-approval", RuntimeLevel: "act-with-approval", Enforced: true}
	res, err := NewApproveTask(h.deps).Execute(context.Background(), task.ID(), DecisionApprove)
	if err != nil {
		t.Fatal(err)
	}
	req := h.decider.requests[0]
	if req.Level != domain.AutonomyActWithApproval || !req.ToolRules.Constrains("local_exec") || len(req.AllowedTools) != 2 {
		t.Errorf("decision request = %+v", req)
	}
	if !res.Execution.Policy().Enforced || h.store.reports[task.ID()] != "done after approval" {
		t.Errorf("execution = %+v report = %q", res.Execution.Snapshot(), h.store.reports[task.ID()])
	}
}

func TestSubmitAlertNamesTheUnitAndTheWindow(t *testing.T) {
	h := newHarness()
	bot := governedBot(t, h, "observe")
	at := time.Date(2026, 10, 24, 10, 37, 12, 0, time.UTC)
	res, err := NewSubmitAlert(h.deps).Execute(context.Background(), AlertCommand{BotID: bot.ID().String(), Unit: "demo-broken", Host: "box", At: at, UserManager: true, CorrelationID: "sd-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || res.Task.IdempotencyKey() != "alert:demo-broken.service:2026-10-24T10:30:00Z" || res.Task.CorrelationID() != "sd-1" {
		t.Errorf("task = %+v", res.Task.Snapshot())
	}
	objective := res.Task.Objective().String()
	for _, want := range []string{"demo-broken.service failed on host box at 2026-10-24T10:37:12Z", "systemctl --user status demo-broken.service", "journalctl --user -u demo-broken.service", "1. Timeline", "2. Services involved", "3. Commands run", "4. Hypotheses, ordered by evidence", "5. Proposed next steps", "Do not change anything"} {
		if !strings.Contains(objective, want) {
			t.Errorf("objective lacks %q", want)
		}
	}
	// The same unit failing again in the window names the same task; the
	// next window names a new one.
	again, err := NewSubmitAlert(h.deps).Execute(context.Background(), AlertCommand{BotID: bot.ID().String(), Unit: "demo-broken.service", At: at.Add(2 * time.Minute)})
	if err != nil || again.Created || again.Task.ID() != res.Task.ID() {
		t.Errorf("repeat alert: %v %+v", err, again)
	}
	next, err := NewSubmitAlert(h.deps).Execute(context.Background(), AlertCommand{BotID: bot.ID().String(), Unit: "demo-broken.service", At: at.Add(10 * time.Minute)})
	if err != nil || !next.Created || next.Task.ID() == res.Task.ID() {
		t.Errorf("next window: %v %+v", err, next)
	}
	// A system unit, now, with a custom window and no host.
	h.clock.now = time.Date(2026, 10, 24, 11, 59, 0, 0, time.UTC)
	sys, err := NewSubmitAlert(h.deps).Execute(context.Background(), AlertCommand{BotID: bot.ID().String(), Unit: "nginx.service", Window: time.Hour})
	if err != nil || sys.Task.IdempotencyKey() != "alert:nginx.service:2026-10-24T11:00:00Z" {
		t.Errorf("system alert: %v %+v", err, sys.Task.Snapshot())
	}
	if o := sys.Task.Objective().String(); !strings.Contains(o, "failed on this host") || !strings.Contains(o, "`systemctl status nginx.service`") || strings.Contains(o, "--user") {
		t.Errorf("system objective = %s", o)
	}
	if _, err := NewSubmitAlert(h.deps).Execute(context.Background(), AlertCommand{BotID: bot.ID().String(), Unit: "  "}); err == nil {
		t.Error("empty unit accepted")
	}
	if _, err := NewSubmitAlert(h.deps).Execute(context.Background(), AlertCommand{BotID: "nobody", Unit: "x"}); !domain.IsNotFound(err) {
		t.Errorf("unknown bot: %v", err)
	}
	if AlertKey("a.service", time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("x", 3600))) != "alert:a.service:2025-12-31T23:00:00Z" {
		t.Error("AlertKey is not UTC")
	}
}

func TestRunNextPendingRunsTheOldest(t *testing.T) {
	h := newHarness()
	bot := enabledBot(t, h)
	res, ran, err := NewRunNextPending(h.deps).Execute(context.Background())
	if err != nil || ran || res.Task.ID() != "" {
		t.Errorf("nothing pending: %v %v", err, ran)
	}
	h.clock.now = t0.Add(time.Minute)
	younger := submit(t, h, bot, "k-young").Task
	h.clock.now = t0
	older := submit(t, h, bot, "k-old").Task
	res, ran, err = NewRunNextPending(h.deps).Execute(context.Background())
	if err != nil || !ran || res.Task.ID() != older.ID() || res.Task.Status() != domain.TaskCompleted {
		t.Errorf("first: %v %v %+v", err, ran, res.Task.Snapshot())
	}
	res, ran, err = NewRunNextPending(h.deps).Execute(context.Background())
	if err != nil || !ran || res.Task.ID() != younger.ID() {
		t.Errorf("second: %v %v %+v", err, ran, res.Task.Snapshot())
	}
	if _, ran, err := NewRunNextPending(h.deps).Execute(context.Background()); err != nil || ran {
		t.Errorf("drained: %v %v", err, ran)
	}
	// A run that cannot start is reported with ran true, so the caller logs it.
	submit(t, h, bot, "k-3")
	h.runtime.err = nil
	h.execs.fail = errBoom
	if _, ran, err := NewRunNextPending(h.deps).Execute(context.Background()); !ran || !errors.Is(err, errBoom) {
		t.Errorf("port error: %v %v", err, ran)
	}
	h.tasks.listFail = errBoom
	if _, ran, err := NewRunNextPending(h.deps).Execute(context.Background()); ran || !errors.Is(err, errBoom) {
		t.Errorf("list error: %v %v", err, ran)
	}
}
