package daemon

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
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
	logs   *bytes.Buffer
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
	return &fixture{dir: dir, state: state, socket: filepath.Join(dir, "cp.sock"), deps: deps, logs: &bytes.Buffer{}}
}

// start runs the daemon with fast ticks and returns a stop function
// that waits for it.
func (f *fixture) start(t *testing.T) (*unixapi.Client, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, f.deps, Options{SocketPath: f.socket, Tick: 20 * time.Millisecond, ReconcileEvery: 50 * time.Millisecond, Grace: time.Millisecond, NodeOfflineAfter: 150 * time.Millisecond, Log: log.New(f.logs, "", 0)})
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
	if !strings.Contains(failed.FailureReason, "reconciled") || !strings.Contains(f.logs.String(), "reconcile: failed ["+failed.ID+"]") {
		t.Errorf("task = %+v logs = %s", failed, f.logs.String())
	}
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
	time.Sleep(80 * time.Millisecond)
	if !strings.Contains(f.logs.String(), "daemon: run:") {
		t.Errorf("logs = %s", f.logs.String())
	}
	if err := os.Remove(filepath.Join(f.state, "tasks", "task-broken.json")); err != nil {
		t.Fatal(err)
	}
	failed := waitFor(t, client, submitted.Task.ID, "Failed")
	if !strings.Contains(f.logs.String(), "task "+failed.ID+" Failed (execution ") || !strings.Contains(f.logs.String(), "no model provider") {
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
	if o.tick() != DefaultTick || o.reconcileEvery() != DefaultReconcileEvery || o.log() == nil {
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
	submitted, _ := client.SubmitTask(ctx, unixapi.SubmitRequest{BotID: "bot", Objective: "o", IdempotencyKey: "k"})
	time.Sleep(100 * time.Millisecond)
	if task, _ := client.GetTask(ctx, submitted.Task.ID); task.Status != "Pending" {
		t.Fatalf("the daemon ran a node-backed task in process: %+v", task)
	}
	assignment, found, err := client.NextAssignment(ctx, "web-1", 0)
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
	if !strings.Contains(f.logs.String(), "nodes offline: [web-1]") {
		t.Errorf("logs = %s", f.logs.String())
	}
	next, _ := client.SubmitTask(ctx, unixapi.SubmitRequest{BotID: "bot", Objective: "o2", IdempotencyKey: "k2"})
	waitFor(t, client, next.Task.ID, "Completed")
	_ = assignment
}
