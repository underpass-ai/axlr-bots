package filestore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/underpass-bots/application"
	"github.com/underpass-ai/underpass-bots/domain"
)

var (
	_ application.BotRepository       = BotRepository{}
	_ application.TaskRepository      = TaskRepository{}
	_ application.ExecutionRepository = ExecutionRepository{}
	_ application.ArtifactStore       = ArtifactStore{}
	_ application.NodeRepository      = NodeRepository{}
)

var t0 = time.Date(2026, 10, 23, 15, 0, 0, 0, time.UTC)

func sampleBot(t *testing.T, id string) domain.Bot {
	t.Helper()
	tools, _ := domain.NewAllowedTools([]string{"observability.query_logs"})
	model, _ := domain.NewModelPolicy("anthropic/claude-haiku-5.5", "", false, 0)
	budgets, _ := domain.NewBudgets(1, 2, 3, time.Minute)
	bot, err := domain.NewBot(domain.BotID(id), "engineering", "Investigator", "platform-team", "Diagnose incidents", tools, model, budgets, t0)
	if err != nil {
		t.Fatal(err)
	}
	return bot
}

func TestBotRepositorySurvivesAReopen(t *testing.T) {
	ctx := context.Background()
	state := t.TempDir()
	bot := sampleBot(t, "sii")
	_ = bot.Register(t0)
	if err := NewBotRepository(state).Save(ctx, bot); err != nil {
		t.Fatal(err)
	}
	// A second repository over the same directory stands for a restart.
	reopened := NewBotRepository(state)
	got, err := reopened.Get(ctx, "sii")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status() != domain.BotRegistered || got.AllowedTools().Len() != 1 || got.Budgets().TaskTimeout() != time.Minute || !got.CreatedAt().Equal(t0) {
		t.Errorf("reloaded bot differs: %+v", got.Snapshot())
	}
	if _, err := reopened.Get(ctx, "missing"); !domain.IsNotFound(err) {
		t.Errorf("missing: %v", err)
	}
	_ = reopened.Save(ctx, sampleBot(t, "aaa"))
	list, err := reopened.List(ctx)
	if err != nil || len(list) != 2 || list[0].ID() != "aaa" || list[1].ID() != "sii" {
		t.Errorf("list: %v %d", err, len(list))
	}
	// Overwriting keeps one file per bot and no temporaries behind.
	_ = got.Enable(t0)
	if err := reopened.Save(ctx, got); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(state, "bots"))
	if len(entries) != 2 {
		t.Errorf("bots dir has %d entries", len(entries))
	}
	again, _ := reopened.Get(ctx, "sii")
	if again.Status() != domain.BotEnabled {
		t.Error("overwrite lost")
	}
}

func TestListOnEmptyAndCorruptStores(t *testing.T) {
	ctx := context.Background()
	state := t.TempDir()
	repo := NewBotRepository(state)
	if list, err := repo.List(ctx); err != nil || len(list) != 0 {
		t.Errorf("empty list: %v %d", err, len(list))
	}
	dir := filepath.Join(state, "bots")
	_ = os.MkdirAll(filepath.Join(dir, "subdir"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".hidden.json"), []byte("{}"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644)
	if list, err := repo.List(ctx); err != nil || len(list) != 0 {
		t.Errorf("list ignores non-aggregates: %v %d", err, len(list))
	}
	_ = os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not json"), 0o644)
	if _, err := repo.Get(ctx, "broken"); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("corrupt file: %v", err)
	}
	if _, err := repo.List(ctx); err == nil {
		t.Error("list with a corrupt file succeeded")
	}
	_ = os.WriteFile(filepath.Join(dir, "invalid.json"), []byte(`{"id":"invalid","status":"Nope"}`), 0o644)
	_ = os.Remove(filepath.Join(dir, "broken.json"))
	if _, err := repo.Get(ctx, "invalid"); err == nil {
		t.Error("invalid snapshot restored")
	}
	// A directory that cannot be listed.
	unreadable := NewBotRepository(filepath.Join(state, "file-not-dir"))
	_ = os.WriteFile(filepath.Join(state, "file-not-dir"), []byte("x"), 0o644)
	if _, err := unreadable.List(ctx); err == nil {
		t.Error("list over a file succeeded")
	}
}

func TestTaskRepositoryFindsByKey(t *testing.T) {
	ctx := context.Background()
	repo := NewTaskRepository(t.TempDir())
	if _, found, err := repo.FindByIdempotencyKey(ctx, "bot-1", "k"); err != nil || found {
		t.Errorf("empty store: %v %v", err, found)
	}
	a, _ := domain.NewTask("task-a", "bot-1", "Investigate", "alert-1", "trace-1", t0)
	b, _ := domain.NewTask("task-b", "bot-2", "Investigate", "alert-1", "", t0)
	_ = b.Start("exec-1", t0)
	_ = b.Fail("boom", t0)
	for _, task := range []domain.Task{a, b} {
		if err := repo.Save(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	got, found, err := repo.FindByIdempotencyKey(ctx, "bot-1", "alert-1")
	if err != nil || !found || got.ID() != "task-a" {
		t.Errorf("find: %v %v %s", err, found, got.ID())
	}
	if _, found, _ := repo.FindByIdempotencyKey(ctx, "bot-1", "alert-2"); found {
		t.Error("found a task under another key")
	}
	if _, found, _ := repo.FindByIdempotencyKey(ctx, "bot-3", "alert-1"); found {
		t.Error("found a task of another bot")
	}
	reloaded, err := repo.Get(ctx, "task-b")
	if err != nil || reloaded.Status() != domain.TaskFailed || reloaded.FailureReason() != "boom" || reloaded.CurrentExecutionID() != "exec-1" {
		t.Errorf("reloaded task: %v %+v", err, reloaded.Snapshot())
	}
	if _, err := repo.Get(ctx, "nope"); !domain.IsNotFound(err) {
		t.Errorf("missing: %v", err)
	}
	_ = os.WriteFile(filepath.Join(repo.files.dir, "bad.json"), []byte("{"), 0o644)
	if _, _, err := repo.FindByIdempotencyKey(ctx, "bot-1", "alert-1"); err == nil {
		t.Error("corrupt task ignored by the scan")
	}
	broken := NewTaskRepository(filepath.Join(t.TempDir(), "f"))
	_ = os.WriteFile(filepath.Dir(broken.files.dir), []byte("x"), 0o644)
	if _, _, err := broken.FindByIdempotencyKey(ctx, "b", "k"); err == nil {
		t.Error("scan over an unreadable directory succeeded")
	}
}

func TestExecutionRepositoryRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := NewExecutionRepository(t.TempDir())
	e, _ := domain.NewExecution("exec-1", "task-1", "bot-1", t0)
	if err := repo.Save(ctx, e); err != nil {
		t.Fatal(err)
	}
	running, err := repo.Get(ctx, "exec-1")
	if err != nil || running.Outcome() != domain.ExecutionRunning {
		t.Errorf("running: %v", err)
	}
	_ = e.Complete(domain.Result{Answer: "answer", SessionID: "s", ToolCalls: []domain.ToolCall{{Name: "local_read", CallID: "c", Status: "completed"}}}, domain.Usage{ToolCalls: 1}, "/trace", t0.Add(time.Second))
	if err := repo.Save(ctx, e); err != nil {
		t.Fatal(err)
	}
	done, err := repo.Get(ctx, "exec-1")
	if err != nil || done.Outcome() != domain.ExecutionCompleted || done.Result().Answer != "answer" || len(done.Result().ToolCalls) != 1 || done.TracePath() != "/trace" {
		t.Errorf("done: %v %+v", err, done.Snapshot())
	}
	if _, err := repo.Get(ctx, "nope"); !domain.IsNotFound(err) {
		t.Errorf("missing: %v", err)
	}
}

func TestWriteAtomicallyFailures(t *testing.T) {
	state := t.TempDir()
	blocker := filepath.Join(state, "blocker")
	_ = os.WriteFile(blocker, []byte("x"), 0o644)
	if err := writeAtomically(filepath.Join(blocker, "x.json"), []byte("{}")); err == nil {
		t.Error("mkdir under a file succeeded")
	}
	target := filepath.Join(state, "dir-target")
	_ = os.MkdirAll(filepath.Join(target, "child"), 0o755)
	if err := writeAtomically(target, []byte("{}")); err == nil {
		t.Error("rename over a non-empty directory succeeded")
	}
	if entries, _ := os.ReadDir(state); len(entries) != 2 {
		t.Errorf("temporaries left behind: %d entries", len(entries))
	}
}

func TestCollectionWriteRejectsUnmarshalable(t *testing.T) {
	c := collection{dir: t.TempDir(), kind: "x"}
	if err := c.write("id", make(chan int)); err == nil {
		t.Error("channel marshalled")
	}
}

func TestTaskRepositoryListsByStatus(t *testing.T) {
	ctx := context.Background()
	repo := NewTaskRepository(t.TempDir())
	if list, err := repo.ListByStatus(ctx, domain.TaskRunning); err != nil || len(list) != 0 {
		t.Errorf("empty store: %v %d", err, len(list))
	}
	pending, _ := domain.NewTask("task-c", "bot-1", "Investigate", "k1", "", t0)
	runningB, _ := domain.NewTask("task-b", "bot-1", "Investigate", "k2", "", t0)
	_ = runningB.Start("exec-b", t0)
	runningA, _ := domain.NewTask("task-a", "bot-2", "Investigate", "k3", "", t0)
	_ = runningA.Start("exec-a", t0)
	for _, task := range []domain.Task{pending, runningB, runningA} {
		if err := repo.Save(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	running, err := repo.ListByStatus(ctx, domain.TaskRunning)
	if err != nil || len(running) != 2 || running[0].ID() != "task-a" || running[1].ID() != "task-b" {
		t.Errorf("running: %v %d", err, len(running))
	}
	if list, err := repo.ListByStatus(ctx, domain.TaskFailed); err != nil || len(list) != 0 {
		t.Errorf("failed: %v %d", err, len(list))
	}
	_ = os.WriteFile(filepath.Join(repo.files.dir, "bad.json"), []byte("{"), 0o644)
	if _, err := repo.ListByStatus(ctx, domain.TaskRunning); err == nil {
		t.Error("corrupt task ignored by the scan")
	}
	broken := NewTaskRepository(filepath.Join(t.TempDir(), "f"))
	_ = os.WriteFile(filepath.Dir(broken.files.dir), []byte("x"), 0o644)
	if _, err := broken.ListByStatus(ctx, domain.TaskRunning); err == nil {
		t.Error("scan over an unreadable directory succeeded")
	}
}

func TestArtifactStoreKeepsTheReport(t *testing.T) {
	state := t.TempDir()
	store := NewArtifactStore(state)
	ctx := context.Background()
	if _, err := store.LoadReport(ctx, "task-1"); !domain.IsNotFound(err) {
		t.Errorf("missing report: %v", err)
	}
	path, err := store.SaveReport(ctx, "task-1", "exec-1", []byte("# Report\n"))
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(state, "artifacts", "task-1", "report.md") {
		t.Errorf("path = %s", path)
	}
	if got, err := store.LoadReport(ctx, "task-1"); err != nil || string(got) != "# Report\n" {
		t.Errorf("load = %q %v", got, err)
	}
	if _, err := store.SaveReport(ctx, "task-1", "exec-2", []byte("second")); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.LoadReport(ctx, "task-1"); string(got) != "second" {
		t.Errorf("second report = %q", got)
	}
	blocked := NewArtifactStore(filepath.Join(state, "file"))
	if err := os.WriteFile(filepath.Join(state, "file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := blocked.SaveReport(ctx, "task-1", "exec-1", nil); err == nil {
		t.Error("write under a file succeeded")
	}
	if _, err := blocked.LoadReport(ctx, "task-1"); err == nil || domain.IsNotFound(err) {
		t.Errorf("read under a file: %v", err)
	}
}

func TestNodeRepositoryAndRunningExecutions(t *testing.T) {
	state := t.TempDir()
	ctx := context.Background()
	nodes := NewNodeRepository(state)
	if list, err := nodes.List(ctx); err != nil || len(list) != 0 {
		t.Errorf("empty list: %v %d", err, len(list))
	}
	labels, _ := domain.NewNodeLabels(map[string]string{"host": "web-1"})
	node, _ := domain.NewNode("web-1", "web-1.example", labels, domain.NodeCapabilities{PolicySupported: true}, t0)
	node.Heartbeat(t0.Add(time.Second))
	if err := nodes.Save(ctx, node); err != nil {
		t.Fatal(err)
	}
	back, err := NewNodeRepository(state).Get(ctx, "web-1")
	if err != nil || back.Status() != domain.NodeOnline || back.Labels()["host"] != "web-1" || !back.Capabilities().PolicySupported {
		t.Errorf("reopened node = %v %+v", err, back.Snapshot())
	}
	if _, err := nodes.Get(ctx, "ghost"); !domain.IsNotFound(err) {
		t.Errorf("missing node: %v", err)
	}
	if list, err := nodes.List(ctx); err != nil || len(list) != 1 {
		t.Errorf("list: %v %d", err, len(list))
	}
	_ = os.WriteFile(filepath.Join(state, "nodes", "bad.json"), []byte("{"), 0o644)
	if _, err := nodes.List(ctx); err == nil {
		t.Error("corrupt node listed")
	}

	execs := NewExecutionRepository(state)
	running, _ := domain.NewExecution("exec-run", "task-1", "bot-1", t0)
	done, _ := domain.NewExecution("exec-done", "task-1", "bot-1", t0)
	_ = done.Complete(domain.Result{}, domain.Usage{}, "", t0)
	for _, e := range []domain.Execution{running, done} {
		if err := execs.Save(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	list, err := execs.ListRunning(ctx)
	if err != nil || len(list) != 1 || list[0].ID() != "exec-run" {
		t.Errorf("running = %v %d", err, len(list))
	}
	if list, err := NewExecutionRepository(filepath.Join(state, "nowhere")).ListRunning(ctx); err != nil || len(list) != 0 {
		t.Errorf("no dir: %v %d", err, len(list))
	}
	_ = os.WriteFile(filepath.Join(state, "executions", "bad.json"), []byte("{"), 0o644)
	if _, err := execs.ListRunning(ctx); err == nil {
		t.Error("corrupt execution listed")
	}
}
