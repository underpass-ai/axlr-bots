package metrics

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/compose"
	"github.com/underpass-ai/axlr-bots/domain"
)

var t0 = time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)

// steppedClock is a clock the test moves.
type steppedClock struct{ now time.Time }

func (c *steppedClock) Now() time.Time          { return c.now }
func (c *steppedClock) advance(d time.Duration) { c.now = c.now.Add(d) }

// scriptedRuntime answers each run or decision with the next report and
// lets the clock run while it "works".
type scriptedRuntime struct {
	clock   *steppedClock
	runs    []application.ExecutionReport
	decides []application.ExecutionReport
	took    time.Duration
}

func (s *scriptedRuntime) Execute(context.Context, application.ExecutionRequest) (application.ExecutionReport, error) {
	report := s.runs[0]
	s.runs = s.runs[1:]
	s.clock.advance(s.took)
	return report, nil
}

func (s *scriptedRuntime) Decide(context.Context, application.DecisionRequest) (application.ExecutionReport, error) {
	report := s.decides[0]
	s.decides = s.decides[1:]
	s.clock.advance(s.took)
	return report, nil
}

type observed struct {
	t        *testing.T
	ctx      context.Context
	deps     application.Dependencies
	clock    *steppedClock
	runtime  *scriptedRuntime
	registry *prometheus.Registry
	observer *TaskObserver
}

func newObserved(t *testing.T) *observed {
	t.Helper()
	deps, err := compose.Dependencies(filepath.Join(t.TempDir(), "state"), "axlr-tui", "test:metrics")
	if err != nil {
		t.Fatal(err)
	}
	clock := &steppedClock{now: t0}
	runtime := &scriptedRuntime{clock: clock, took: 6 * time.Second}
	deps.Clock, deps.Runtime, deps.Decisions = clock, runtime, runtime
	series := NewTaskMetrics()
	registry := prometheus.NewRegistry()
	observer := NewTaskObserver(deps.Events, deps.Tasks, deps.Executions, series)
	registry.MustRegister(observer)
	deps.Events = observer
	return &observed{t: t, ctx: context.Background(), deps: deps, clock: clock, runtime: runtime, registry: registry, observer: observer}
}

func (o *observed) bot(name string, selector map[string]string) domain.Bot {
	o.t.Helper()
	definition := application.BotDefinition{
		Tenant: "ops", Name: name, Owner: "platform", Mission: "do what the task asks", AllowedTools: []string{"local_read"},
		Placement: domain.PlacementSnapshot{Selector: selector},
		Budgets:   application.BudgetsDefinition{MaxModelCallsPerTask: 10, MaxToolCallsPerTask: 10, MaxDelegationDepth: 1, TaskTimeoutSeconds: 900},
	}
	bot, err := application.NewRegisterBot(o.deps).Execute(o.ctx, definition)
	if err != nil {
		o.t.Fatal(err)
	}
	if bot, err = application.NewEnableBot(o.deps).Execute(o.ctx, bot.ID()); err != nil {
		o.t.Fatal(err)
	}
	return bot
}

func (o *observed) submit(bot domain.Bot, key string) domain.Task {
	o.t.Helper()
	result, err := application.NewSubmitTask(o.deps).Execute(o.ctx, application.SubmitTaskCommand{BotID: bot.ID().String(), Objective: "look at " + key, IdempotencyKey: key})
	if err != nil {
		o.t.Fatal(err)
	}
	return result.Task
}

func completed(cost float64, prompt, completion int) application.ExecutionReport {
	return application.ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done", SessionID: "s"}, Usage: domain.Usage{Requests: 1, PromptTokens: prompt, CachedTokens: 5, CompletionTokens: completion, ReasoningTokens: 2, CostUSD: cost}}
}

// family finds a metric family by name in a scrape.
func (o *observed) family(name string) *dto.MetricFamily {
	o.t.Helper()
	families, err := o.registry.Gather()
	if err != nil {
		o.t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() == name {
			return family
		}
	}
	return nil
}

// metric finds the series of a family whose labels are exactly want.
func (o *observed) metric(name string, want map[string]string) *dto.Metric {
	o.t.Helper()
	family := o.family(name)
	if family == nil {
		return nil
	}
	for _, metric := range family.GetMetric() {
		if len(metric.GetLabel()) != len(want) {
			continue
		}
		match := true
		for _, label := range metric.GetLabel() {
			if want[label.GetName()] != label.GetValue() {
				match = false
			}
		}
		if match {
			return metric
		}
	}
	return nil
}

func (o *observed) counter(name string, want map[string]string) float64 {
	o.t.Helper()
	if metric := o.metric(name, want); metric != nil {
		return metric.GetCounter().GetValue()
	}
	return 0
}

// total sums a counter family over every series.
func (o *observed) total(name string) float64 {
	o.t.Helper()
	var sum float64
	if family := o.family(name); family != nil {
		for _, metric := range family.GetMetric() {
			sum += metric.GetCounter().GetValue()
		}
	}
	return sum
}

func (o *observed) histogram(name string, want map[string]string) (count uint64, sum float64) {
	o.t.Helper()
	if metric := o.metric(name, want); metric != nil {
		return metric.GetHistogram().GetSampleCount(), metric.GetHistogram().GetSampleSum()
	}
	return 0, 0
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func (o *observed) overviewCost() float64 {
	o.t.Helper()
	overview, err := application.NewGetOverview(o.deps).Execute(o.ctx, t0.Add(-time.Hour))
	if err != nil {
		o.t.Fatal(err)
	}
	return overview.CostUSDSince
}

func TestInProcessTasksAreMeasuredPerBot(t *testing.T) {
	o := newObserved(t)
	bot := o.bot("alpha", nil)
	o.runtime.runs = []application.ExecutionReport{
		completed(0.01, 100, 20),
		{Outcome: domain.ExecutionFailed, FailureReason: "boom", Usage: domain.Usage{Requests: 1, PromptTokens: 7, CostUSD: 0.03}},
	}
	first := o.submit(bot, "k1")
	o.clock.advance(4 * time.Second)
	if _, err := application.NewRunTask(o.deps).Execute(o.ctx, first.ID()); err != nil {
		t.Fatal(err)
	}
	second := o.submit(bot, "k2")
	if _, err := application.NewRunTask(o.deps).Execute(o.ctx, second.ID()); err != nil {
		t.Fatal(err)
	}

	alpha := map[string]string{"bot": "alpha"}
	if count, sum := o.histogram("axlr_bots_task_queue_seconds", alpha); count != 2 || !near(sum, 4) {
		t.Errorf("queue = %d %v, want 2 and 4s (4s, then straight away)", count, sum)
	}
	if count, sum := o.histogram("axlr_bots_task_duration_seconds", map[string]string{"bot": "alpha", "state": "Completed"}); count != 1 || !near(sum, 10) {
		t.Errorf("completed duration = %d %v, want 1 and 10s", count, sum)
	}
	if count, sum := o.histogram("axlr_bots_task_duration_seconds", map[string]string{"bot": "alpha", "state": "Failed"}); count != 1 || !near(sum, 6) {
		t.Errorf("failed duration = %d %v, want 1 and 6s", count, sum)
	}
	if count, sum := o.histogram("axlr_bots_execution_duration_seconds", map[string]string{"bot": "alpha", "outcome": "Completed"}); count != 1 || !near(sum, 6) {
		t.Errorf("execution duration = %d %v", count, sum)
	}
	if got := o.counter("axlr_bots_tasks_finished_total", map[string]string{"bot": "alpha", "state": "Failed"}); got != 1 {
		t.Errorf("failed tasks = %v", got)
	}
	for kind, want := range map[string]float64{"prompt": 107, "cached": 5, "completion": 20, "reasoning": 2} {
		if got := o.counter("axlr_bots_tokens_total", map[string]string{"bot": "alpha", "kind": kind}); got != want {
			t.Errorf("tokens %s = %v, want %v", kind, got, want)
		}
	}
	if got := o.counter("axlr_bots_cost_usd_total", alpha); !near(got, 0.04) {
		t.Errorf("cost = %v, want 0.04", got)
	}
}

func TestApprovalWaitIsMeasuredFromTheWaitToItsClose(t *testing.T) {
	o := newObserved(t)
	bot := o.bot("alpha", nil)
	o.runtime.runs = []application.ExecutionReport{{
		Outcome: domain.ExecutionDecisionRequired,
		Result:  domain.Result{SessionID: "s", PendingDecision: &domain.PendingDecision{Tool: "local_write", CallID: "c1", SessionID: "s"}},
		Usage:   domain.Usage{Requests: 1, CostUSD: 0.04},
	}}
	o.runtime.decides = []application.ExecutionReport{completed(0.05, 10, 1)}
	task := o.submit(bot, "k")
	if _, err := application.NewRunTask(o.deps).Execute(o.ctx, task.ID()); err != nil {
		t.Fatal(err)
	}
	if count, _ := o.histogram("axlr_bots_execution_duration_seconds", map[string]string{"bot": "alpha", "outcome": "DecisionRequired"}); count != 1 {
		t.Errorf("the execution that stopped on a call was not measured")
	}
	o.clock.advance(30 * time.Second)
	if _, err := application.NewApproveTask(o.deps).Execute(o.ctx, task.ID(), application.DecisionDeny, "", ""); err != nil {
		t.Fatal(err)
	}
	if count, sum := o.histogram("axlr_bots_approval_wait_seconds", map[string]string{"bot": "alpha", "outcome": "denied"}); count != 1 || !near(sum, 30) {
		t.Errorf("approval wait = %d %v, want 1 and 30s", count, sum)
	}
	if got := o.counter("axlr_bots_cost_usd_total", map[string]string{"bot": "alpha"}); !near(got, 0.09) {
		t.Errorf("cost over both executions = %v, want 0.09", got)
	}
	if got := o.counter("axlr_bots_tasks_finished_total", map[string]string{"bot": "alpha", "state": "Completed"}); got != 1 {
		t.Errorf("finished = %v: a wait is not an end", got)
	}
}

func TestACancelledWaitAndAnExpiredOneCountOnce(t *testing.T) {
	o := newObserved(t)
	bot := o.bot("alpha", nil)
	pending := application.ExecutionReport{
		Outcome: domain.ExecutionDecisionRequired,
		Result:  domain.Result{SessionID: "s", PendingDecision: &domain.PendingDecision{Tool: "local_write", CallID: "c1", SessionID: "s"}},
	}
	o.runtime.runs = []application.ExecutionReport{pending, pending}
	task := o.submit(bot, "k")
	if _, err := application.NewRunTask(o.deps).Execute(o.ctx, task.ID()); err != nil {
		t.Fatal(err)
	}
	o.clock.advance(20 * time.Second)
	if _, err := application.NewCancelTask(o.deps).Execute(o.ctx, task.ID()); err != nil {
		t.Fatal(err)
	}
	// Cancelling a task whose wait ended long ago adds no wait.
	again := o.submit(bot, "k2")
	if _, err := application.NewRunTask(o.deps).Execute(o.ctx, again.ID()); err != nil {
		t.Fatal(err)
	}
	if count, sum := o.histogram("axlr_bots_approval_wait_seconds", map[string]string{"bot": "alpha", "outcome": "cancelled"}); count != 1 || !near(sum, 20) {
		t.Errorf("cancelled wait = %d %v, want 1 and 20s", count, sum)
	}
	o.clock.advance(24 * time.Hour)
	o.runtime.decides = []application.ExecutionReport{completed(0, 0, 0)}
	if _, err := application.NewExpireApprovals(o.deps).Expire(o.ctx, again.ID()); err != nil {
		t.Fatal(err)
	}
	if count, _ := o.histogram("axlr_bots_approval_wait_seconds", map[string]string{"bot": "alpha", "outcome": "expired"}); count != 1 {
		t.Errorf("expired waits = %d, want 1 (the expiry and the denial it carried are one wait)", count)
	}
	if got := o.counter("axlr_bots_tasks_finished_total", map[string]string{"bot": "alpha", "state": "Cancelled"}); got != 1 {
		t.Errorf("cancelled tasks = %v", got)
	}
}

// The counter of cost follows the overview's cost over several
// executions, on both paths a run takes (in process, and on a node), and
// the late report of a cancelled node run is counted when it is
// scraped: it publishes no event.
func TestCostCounterFollowsTheOverviewAcrossExecutionsAndLateReports(t *testing.T) {
	o := newObserved(t)
	alpha := o.bot("alpha", nil)
	beta := o.bot("beta", map[string]string{"host": "n1"})
	o.runtime.runs = []application.ExecutionReport{completed(0.01, 10, 1), completed(0.02, 20, 2)}
	for _, key := range []string{"a1", "a2"} {
		if _, err := application.NewRunTask(o.deps).Execute(o.ctx, o.submit(alpha, key).ID()); err != nil {
			t.Fatal(err)
		}
	}
	node, err := application.NewRegisterNode(o.deps).Execute(o.ctx, application.NodeRegistration{ID: "n1", HostName: "n1.example", Labels: map[string]string{"host": "n1"}, Capabilities: domain.NodeCapabilities{AXLRVersion: "0.14.0", PolicySupported: true}})
	if err != nil {
		t.Fatal(err)
	}
	reported := o.submit(beta, "b1")
	assignment, found, err := application.NewNextAssignment(o.deps).Execute(o.ctx, node.ID())
	if err != nil || !found {
		t.Fatalf("assignment: %v %v", err, found)
	}
	o.clock.advance(9 * time.Second)
	reply, err := application.NewReportExecution(o.deps).Execute(o.ctx, node.ID(), assignment.ExecutionID, completed(0.1, 30, 3))
	if err != nil || !reply.Accepted || reply.Task.ID() != reported.ID() {
		t.Fatalf("report: %+v %v", reply, err)
	}
	if got := o.counter("axlr_bots_cost_usd_total", map[string]string{"bot": "beta"}); !near(got, 0.1) {
		t.Errorf("the node's reported cost = %v, want 0.1", got)
	}

	cancelled := o.submit(beta, "b2")
	second, found, err := application.NewNextAssignment(o.deps).Execute(o.ctx, node.ID())
	if err != nil || !found {
		t.Fatalf("second assignment: %v %v", err, found)
	}
	if _, err := application.NewCancelTask(o.deps).Execute(o.ctx, cancelled.ID()); err != nil {
		t.Fatal(err)
	}
	if got := o.counter("axlr_bots_executions_cost_unknown_total", map[string]string{"bot": "beta"}); got != 1 {
		t.Fatalf("cost unknown = %v, want 1", got)
	}
	// Scraping before the late report finds nothing new, and counts nothing twice.
	if _, err := o.registry.Gather(); err != nil {
		t.Fatal(err)
	}
	late := application.ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "interrupted", Usage: domain.Usage{Requests: 2, PromptTokens: 40, CostUSD: 0.2}}
	if reply, err := application.NewReportExecution(o.deps).Execute(o.ctx, node.ID(), second.ExecutionID, late); err != nil || reply.Accepted {
		t.Fatalf("late report: %+v %v", reply, err)
	}
	for scrape := 0; scrape < 2; scrape++ {
		if got, want := o.total("axlr_bots_cost_usd_total"), o.overviewCost(); !near(got, want) || !near(want, 0.33) {
			t.Fatalf("scrape %d: counter %v, overview %v, want both 0.33", scrape, got, want)
		}
	}
	if got := o.counter("axlr_bots_tokens_total", map[string]string{"bot": "beta", "kind": "prompt"}); got != 70 {
		t.Errorf("prompt tokens of beta = %v, want 70 (30 reported, 40 late)", got)
	}
	if got := o.counter("axlr_bots_executions_cost_unknown_total", map[string]string{"bot": "beta"}); got != 1 {
		t.Errorf("cost unknown = %v: a late cost does not count the execution again", got)
	}
	if count, _ := o.histogram("axlr_bots_execution_duration_seconds", map[string]string{"bot": "beta", "outcome": "Cancelled"}); count != 1 {
		t.Errorf("cancelled executions measured = %d, want 1", count)
	}
}

func TestToolCallsComeFromProgressEvents(t *testing.T) {
	o := newObserved(t)
	progress := func(items any) domain.Event {
		return domain.Event{ID: "evt-1", SchemaVersion: domain.EventSchemaV1, Type: domain.EventTaskProgress, TenantID: "ops", BotID: "alpha", TaskID: "task-1", ExecutionID: "exec-1", CreatedAt: t0, Payload: map[string]any{"items": items}}
	}
	typed := []application.ProgressItem{
		{Kind: application.ProgressToolStarted, Tool: "local_read"},
		{Kind: application.ProgressToolEnded, Tool: "local_read", Status: "ok", DurationMS: 250},
		{Kind: application.ProgressToolEnded, Tool: "local_exec", Status: "denied"},
		{Kind: application.ProgressText, Text: "words"},
	}
	if err := o.deps.Events.Publish(o.ctx, progress(typed)); err != nil {
		t.Fatal(err)
	}
	// An event that went through JSON reads the same.
	replayed := []any{map[string]any{"kind": "tool.ended", "tool": "x", "status": "failed", "durationMs": 1500}, map[string]any{"kind": "tool.ended", "tool": "y", "status": "weird"}}
	if err := o.deps.Events.Publish(o.ctx, progress(replayed)); err != nil {
		t.Fatal(err)
	}
	for status, want := range map[string]float64{"ok": 1, "denied": 1, "failed": 1, "other": 1} {
		if got := o.counter("axlr_bots_tool_calls_total", map[string]string{"bot": "alpha", "status": status}); got != want {
			t.Errorf("tool calls %s = %v, want %v", status, got, want)
		}
	}
	if count, sum := o.histogram("axlr_bots_tool_call_duration_seconds", map[string]string{"bot": "alpha"}); count != 2 || !near(sum, 1.75) {
		t.Errorf("tool duration = %d %v, want 2 and 1.75s (calls with no duration are counted, not timed)", count, sum)
	}
}

type failingPublisher struct{ err error }

func (f failingPublisher) Publish(context.Context, domain.Event) error { return f.err }

func TestAFailedPublishObservesNothingAndAnUnreadableStoreNeverFailsOne(t *testing.T) {
	o := newObserved(t)
	boom := errors.New("boom")
	series := NewTaskMetrics()
	registry := prometheus.NewRegistry()
	failing := NewTaskObserver(failingPublisher{boom}, o.deps.Tasks, o.deps.Executions, series)
	ended := domain.Event{ID: "evt-1", SchemaVersion: domain.EventSchemaV1, Type: domain.EventTaskCompleted, TenantID: "ops", BotID: "alpha", TaskID: "task-1", ExecutionID: "exec-1", CreatedAt: t0}
	if err := failing.Publish(o.ctx, ended); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	registry.MustRegister(failing)
	if families, _ := registry.Gather(); len(families) != 0 {
		t.Errorf("a failed publish was observed: %d families", len(families))
	}
	// The task and the execution are in no store: the event is counted as
	// ended, and nothing that needs them is invented.
	if err := o.deps.Events.Publish(o.ctx, ended); err != nil {
		t.Fatal(err)
	}
	if got := o.counter("axlr_bots_tasks_finished_total", map[string]string{"bot": "alpha", "state": "Completed"}); got != 1 {
		t.Errorf("finished = %v", got)
	}
	if o.family("axlr_bots_task_duration_seconds") != nil || o.family("axlr_bots_execution_duration_seconds") != nil {
		t.Error("a duration was invented for a task nobody stored")
	}
}

func TestNoLabelNamesATaskAnExecutionOrACorrelation(t *testing.T) {
	o := newObserved(t)
	bot := o.bot("alpha", nil)
	o.runtime.runs = []application.ExecutionReport{completed(0.01, 1, 1)}
	task := o.submit(bot, "k")
	if _, err := application.NewRunTask(o.deps).Execute(o.ctx, task.ID()); err != nil {
		t.Fatal(err)
	}
	families, err := o.registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"bot": true, "state": true, "outcome": true, "kind": true, "status": true, "le": true}
	seen := 0
	for _, family := range families {
		if !strings.HasPrefix(family.GetName(), "axlr_bots_") {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				seen++
				if !allowed[label.GetName()] || strings.Contains(label.GetValue(), "task-") || strings.Contains(label.GetValue(), "exec-") || strings.Contains(label.GetValue(), task.CorrelationID().String()) {
					t.Errorf("%s has label %s=%q", family.GetName(), label.GetName(), label.GetValue())
				}
			}
		}
	}
	if seen == 0 {
		t.Fatal("no series to look at")
	}
}

func TestTheLedgerForgetsTheOldestAndRemembersWhatWaitsForACost(t *testing.T) {
	ledger := newExecutionLedger()
	if !ledger.count("exec-0", true) || ledger.count("exec-0", false) {
		t.Fatal("an execution is counted once")
	}
	if counted, unknown := ledger.state("exec-0"); !counted || !unknown {
		t.Errorf("state = %v %v", counted, unknown)
	}
	if !ledger.settle("exec-0") || ledger.settle("exec-0") {
		t.Error("a late cost is claimed once")
	}
	if ledger.settle("nobody") {
		t.Error("claimed the cost of an execution nobody counted")
	}
	ledger.count("exec-u", true)
	for i := 0; i < ledgerCapacity; i++ {
		ledger.count(domain.ExecutionID("fill-"+strconv.Itoa(i)), false)
	}
	if counted, _ := ledger.state("exec-0"); counted {
		t.Error("the oldest is still remembered past the capacity")
	}
	if len(ledger.unknown()) != 0 {
		t.Errorf("unknown = %v: the evicted are forgotten", ledger.unknown())
	}
}
