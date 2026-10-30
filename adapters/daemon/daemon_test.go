package daemon

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/compose"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/internal/fakeaxlr"
)

type fixture struct {
	dir    string
	state  string
	socket string
	deps   application.Dependencies
	logs   *syncBuffer
}

// syncBuffer is a log sink the daemon's goroutines write and the test
// reads.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newFixture(t *testing.T, scenario string) *fixture {
	t.Helper()
	dir := t.TempDir()
	bin, _, err := fakeaxlr.Write(dir, fakeaxlr.Options{Scenario: scenario})
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "state")
	deps, err := compose.Dependencies(state, bin, "test:daemon")
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{dir: dir, state: state, socket: filepath.Join(dir, "cp.sock"), deps: deps, logs: &syncBuffer{}}
}

// start runs the daemon with fast ticks and returns a stop function
// that waits for it.
func (f *fixture) start(t *testing.T) (*unixapi.Client, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, f.deps, Options{SocketPath: f.socket, Tick: 20 * time.Millisecond, ReconcileEvery: 50 * time.Millisecond, DeliverEvery: 20 * time.Millisecond, Grace: time.Millisecond, NodeOfflineAfter: 150 * time.Millisecond, Log: log.New(f.logs, "", 0)})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !unixapi.Reachable(context.Background(), f.socket) {
		if time.Now().After(deadline) {
			t.Fatal("daemon not reachable")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return unixapi.NewClient(f.socket), func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
}

func waitFor(t *testing.T, client *unixapi.Client, id string, status string) domain.TaskSnapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		task, err := client.GetTask(context.Background(), id)
		if err == nil && task.Status == status {
			return task
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %s is %s (%v), want %s", id, task.Status, err, status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitForLog waits for the daemon to log text: it logs a pass after the
// pass has saved what it changed, so a test that saw the change may
// read the log before the line is there.
func waitForLog(t *testing.T, f *fixture, text string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(f.logs.String(), text) {
		if time.Now().After(deadline) {
			t.Fatalf("no %q in the logs: %s", text, f.logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDaemonRunsWhatArrivesOverTheSocketAndSurvivesARestart(t *testing.T) {
	f := newFixture(t, "completed")
	client, stop := f.start(t)
	ctx := context.Background()
	if _, err := client.RegisterBot(ctx, application.BotDefinition{Tenant: "ops", Name: "bot", Owner: "sre", Mission: "diagnose", AllowedTools: []string{"local_read"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "bot"); err != nil {
		t.Fatal(err)
	}
	alert, err := client.SubmitTask(ctx, unixapi.SubmitRequest{BotID: "bot", Objective: "List the files in your workspace", IdempotencyKey: "k-1"})
	if err != nil || !alert.Created {
		t.Fatalf("submit: %v %+v", err, alert)
	}
	done := waitFor(t, client, alert.Task.ID, "Completed")
	report, err := client.GetReport(ctx, done.ID)
	if err != nil || !strings.HasPrefix(report.Content, "# Report") {
		t.Fatalf("report: %v %+v", err, report)
	}
	if _, err := os.Stat(report.Reference); err != nil {
		t.Error("report file missing:", err)
	}
	// The log line follows the task's save by a hair: wait for it.
	logDeadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(f.logs.String(), "task "+done.ID+" Completed") && time.Now().Before(logDeadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(f.logs.String(), "task "+done.ID+" Completed") || !strings.Contains(f.logs.String(), "report "+report.Reference) {
		t.Errorf("logs = %s", f.logs.String())
	}
	stop()
	if _, err := os.Stat(f.socket); err == nil {
		t.Error("socket left after stop")
	}

	// Restart: the bot and the task are still there, and a task spooled
	// straight into the state directory while the daemon was down runs.
	local := unixapi.NewLocal(f.deps)
	spooled, err := local.SubmitTask(ctx, unixapi.SubmitRequest{BotID: "bot", Objective: "spooled while down", IdempotencyKey: "spool-1"})
	if err != nil || !spooled.Created {
		t.Fatal(err)
	}
	client, stop = f.start(t)
	defer stop()
	if bot, err := client.GetBot(ctx, "bot"); err != nil || bot.Status != "Enabled" {
		t.Errorf("bot after restart: %v %+v", err, bot)
	}
	if task, err := client.GetTask(ctx, done.ID); err != nil || task.Status != "Completed" {
		t.Errorf("task after restart: %v %+v", err, task)
	}
	waitFor(t, client, spooled.Task.ID, "Completed")
	if repeat, err := client.SubmitTask(ctx, unixapi.SubmitRequest{BotID: "bot", Objective: "List the files in your workspace", IdempotencyKey: "k-1"}); err != nil || repeat.Created || repeat.Task.ID != alert.Task.ID {
		t.Errorf("repeated submission after restart: %v %+v", err, repeat)
	}
}

func TestDaemonReconcilesWhatACrashLeftRunning(t *testing.T) {
	f := newFixture(t, "completed")
	ctx := context.Background()
	local := unixapi.NewLocal(f.deps)
	if _, err := local.RegisterBot(ctx, application.BotDefinition{Tenant: "ops", Name: "b", Owner: "o", Mission: "m", Budgets: application.BudgetsDefinition{TaskTimeoutSeconds: 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := local.EnableBot(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	submitted, err := local.SubmitTask(ctx, unixapi.SubmitRequest{BotID: "b", Objective: "o", IdempotencyKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	// Forge the crash: the task and its execution saved as Running long ago.
	task, _ := f.deps.Tasks.Get(ctx, domain.TaskID(submitted.Task.ID))
	started := time.Now().Add(-time.Hour)
	if err := task.Start("exec-lost", started); err != nil {
		t.Fatal(err)
	}
	execution, _ := domain.NewExecution("exec-lost", task.ID(), task.BotID(), started)
	if err := f.deps.Executions.Save(ctx, execution); err != nil {
		t.Fatal(err)
	}
	if err := f.deps.Tasks.Save(ctx, task); err != nil {
		t.Fatal(err)
	}
	client, stop := f.start(t)
	defer stop()
	failed := waitFor(t, client, submitted.Task.ID, "Failed")
	if !strings.Contains(failed.FailureReason, "reconciled") {
		t.Errorf("task = %+v", failed)
	}
	waitForLog(t, f, "reconcile: failed ["+failed.ID+"]")
}

func TestDaemonLogsRunErrorsAndKeepsGoing(t *testing.T) {
	f := newFixture(t, "failed")
	ctx := context.Background()
	local := unixapi.NewLocal(f.deps)
	if _, err := local.RegisterBot(ctx, application.BotDefinition{Tenant: "ops", Name: "b", Owner: "o", Mission: "m"}); err != nil {
		t.Fatal(err)
	}
	if _, err := local.EnableBot(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	submitted, _ := local.SubmitTask(ctx, unixapi.SubmitRequest{BotID: "b", Objective: "o", IdempotencyKey: "k"})
	// A task file that is not valid JSON makes the Pending scan fail; the
	// daemon logs it and keeps serving.
	if err := os.WriteFile(filepath.Join(f.state, "tasks", "task-broken.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	client, stop := f.start(t)
	// The run tick and the reconcile pass both read the broken file; on a
	// loaded runner the reconcile error can be the only line after a fixed
	// sleep (25 Oct 2026: 2 of 3 full-suite runs). Wait for the run's own line.
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(f.logs.String(), "daemon: run:") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(f.logs.String(), "daemon: run:") {
		t.Errorf("logs = %s", f.logs.String())
	}
	if err := os.Remove(filepath.Join(f.state, "tasks", "task-broken.json")); err != nil {
		t.Fatal(err)
	}
	failed := waitFor(t, client, submitted.Task.ID, "Failed")
	waitForLog(t, f, "task "+failed.ID+" Failed (execution ")
	if !strings.Contains(f.logs.String(), "no model provider") {
		t.Errorf("logs = %s", f.logs.String())
	}
	stop()
}

func TestDaemonRefusals(t *testing.T) {
	f := newFixture(t, "completed")
	if err := Run(context.Background(), f.deps, Options{}); err == nil {
		t.Error("no socket path accepted")
	}
	file := filepath.Join(f.dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), f.deps, Options{SocketPath: filepath.Join(file, "x.sock")}); err == nil {
		t.Error("socket under a file served")
	}
	o := Options{}
	if o.tick() != DefaultTick || o.reconcileEvery() != DefaultReconcileEvery || o.deliverEvery() != DefaultDeliverEvery || o.log() == nil {
		t.Error("defaults wrong")
	}
}

func TestDaemonLeavesNodeBackedTasksToTheNodeAndMarksItOffline(t *testing.T) {
	f := newFixture(t, "completed")
	client, stop := f.start(t)
	defer stop()
	ctx := context.Background()
	if _, err := client.RegisterBot(ctx, application.BotDefinition{Tenant: "t", Name: "bot", Owner: "o", Mission: "m"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "bot"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Register(ctx, application.NodeRegistration{ID: "web-1", HostName: "web-1"}); err != nil {
		t.Fatal(err)
	}
	// The node stays alive until it takes the task: the fixture marks a
	// node silent for 150 ms Offline, which a slow runner reached within
	// the 100 ms wait below (25 Oct 2026, CI).
	beating, stopBeating := context.WithCancel(ctx)
	beaten := make(chan struct{})
	go func() {
		defer close(beaten)
		for beating.Err() == nil {
			_, _ = client.Heartbeat(beating, "web-1")
			select {
			case <-beating.Done():
			case <-time.After(30 * time.Millisecond):
			}
		}
	}()
	submitted, _ := client.SubmitTask(ctx, unixapi.SubmitRequest{BotID: "bot", Objective: "o", IdempotencyKey: "k"})
	time.Sleep(100 * time.Millisecond)
	if task, _ := client.GetTask(ctx, submitted.Task.ID); task.Status != "Pending" {
		stopBeating()
		<-beaten
		t.Fatalf("the daemon ran a node-backed task in process: %+v", task)
	}
	// Ask as the node does, with a long poll: with no wait, a loaded runner
	// can ask a moment before the task is ready for the node (25 Oct 2026,
	// CI: "assignment: <nil> false").
	assignment, found, err := client.NextAssignment(ctx, "web-1", 2*time.Second)
	stopBeating()
	<-beaten
	if err != nil || !found || assignment.Run.TaskID != domain.TaskID(submitted.Task.ID) {
		t.Fatalf("assignment: %v %v", err, found)
	}
	// The node goes silent: its lease expires, the task fails, the node is Offline,
	// and the next task runs in process again.
	deadline := time.Now().Add(10 * time.Second)
	for {
		node, _ := client.GetNode(ctx, "web-1")
		if node.Status == "Offline" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("node still %s: %s", node.Status, f.logs.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	// The node is saved Offline before the daemon logs it: wait for the
	// line rather than read the logs once (30 Oct 2026, full suite under
	// -race).
	for !strings.Contains(f.logs.String(), "nodes offline: [web-1]") {
		if time.Now().After(deadline) {
			t.Fatalf("logs = %s", f.logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	next, _ := client.SubmitTask(ctx, unixapi.SubmitRequest{BotID: "bot", Objective: "o2", IdempotencyKey: "k2"})
	waitFor(t, client, next.Task.ID, "Completed")
	_ = assignment
}

// timerDefinition is a bot with a timer every ten minutes.
func timerDefinition(name string) application.BotDefinition {
	return application.BotDefinition{
		Tenant: "ops", Name: name, Owner: "operator", Mission: "do what each task asks, within the policy",
		AllowedTools: []string{"local_read"},
		Triggers:     []domain.TriggerSnapshot{{Type: "manual"}, {Type: "timer", Name: "tick", Objective: "say the time at {{.scheduledAt}}", Every: "10m"}},
		Model:        application.ModelPolicyDefinition{Model: "anthropic/claude-haiku-5.5"},
		Budgets:      application.BudgetsDefinition{TaskTimeoutSeconds: 60},
	}
}

func TestDaemonFiresTimersAndCatchesUpOnceAfterARestart(t *testing.T) {
	f := newFixture(t, "completed")
	ctx := context.Background()
	local := unixapi.NewLocal(f.deps)
	if _, err := local.RegisterBot(ctx, timerDefinition("ticker")); err != nil {
		t.Fatal(err)
	}
	if _, err := local.EnableBot(ctx, "ticker"); err != nil {
		t.Fatal(err)
	}
	// First start: the interval timer is armed and fires at once; the
	// task runs in process and completes.
	client, stop := f.start(t)
	deadline := time.Now().Add(5 * time.Second)
	var first []domain.Event
	for {
		events, _ := client.ListEvents(ctx, "ticker", 0)
		first = eventsOf(events, domain.EventTriggerFired)
		if len(first) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the timer did not fire: %s", f.logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	taskID := first[0].Payload["taskId"].(string)
	waitFor(t, client, taskID, "Completed")
	task, _ := client.GetTask(ctx, taskID)
	if task.Trigger.Type != "timer" || task.Trigger.Name != "tick" || !strings.HasPrefix(task.IdempotencyKey, "timer:tick:") || !strings.HasPrefix(task.Objective, "say the time at 20") {
		t.Errorf("timer task = %+v", task)
	}
	timer, err := f.deps.Timers.Get(ctx, domain.TimerKey("ticker", "tick"))
	if err != nil || timer.LastTaskID().String() != taskID || time.Until(timer.NextFireAt()) < 9*time.Minute {
		t.Errorf("timer = %v %+v", err, timer.Snapshot())
	}
	stop()
	if !strings.Contains(f.logs.String(), "daemon: timer ticker/tick fired") {
		t.Errorf("logs = %s", f.logs.String())
	}

	// The daemon is down past several slots: on restart it fires once for
	// the persisted slot and arms the next one after now.
	past := timer
	if err := past.Rearm(past.ScheduleKey(), time.Now().Add(-35*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := f.deps.Timers.Save(ctx, past); err != nil {
		t.Fatal(err)
	}
	client, stop = f.start(t)
	defer stop()
	deadline = time.Now().Add(5 * time.Second)
	for {
		events, _ := client.ListEvents(ctx, "ticker", 0)
		if fired := eventsOf(events, domain.EventTriggerFired); len(fired) >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no catch-up fire: %s", f.logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	// A few more ticks change nothing.
	time.Sleep(150 * time.Millisecond)
	events, _ := client.ListEvents(ctx, "ticker", 0)
	fired := eventsOf(events, domain.EventTriggerFired)
	if len(fired) != 2 || fired[1].Payload["taskId"] == taskID || fired[1].Payload["created"] != true {
		t.Errorf("fired events = %+v", fired)
	}
	after, _ := f.deps.Timers.Get(ctx, domain.TimerKey("ticker", "tick"))
	if time.Until(after.NextFireAt()) < 9*time.Minute {
		t.Errorf("next fire not after now: %s", after.NextFireAt())
	}
	// The same persisted slot fired again (a crash before the save) finds the same task.
	tasks, _ := f.deps.Tasks.ListByStatus(ctx, domain.TaskCompleted)
	pending, _ := f.deps.Tasks.ListByStatus(ctx, domain.TaskPending)
	running, _ := f.deps.Tasks.ListByStatus(ctx, domain.TaskRunning)
	if total := len(tasks) + len(pending) + len(running); total != 2 {
		t.Errorf("%d tasks, want 2", total)
	}
}

func eventsOf(events []domain.Event, kind domain.EventType) []domain.Event {
	var out []domain.Event
	for _, e := range events {
		if e.Type == kind {
			out = append(out, e)
		}
	}
	return out
}
