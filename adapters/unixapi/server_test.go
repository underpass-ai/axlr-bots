package unixapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/underpass-bots/application"
	"github.com/underpass-ai/underpass-bots/compose"
	"github.com/underpass-ai/underpass-bots/domain"
	"github.com/underpass-ai/underpass-bots/internal/fakeaxlr"
)

var (
	_ Service = (*Local)(nil)
	_ Service = (*Client)(nil)
)

// serve starts a daemon-like server on a socket under a short temp path
// with the fake axlr-tui in the scenario, and returns a client to it.
func serve(t *testing.T, scenario string) (*Client, *Local, string) {
	t.Helper()
	dir := t.TempDir()
	bin, _, err := fakeaxlr.Write(dir, fakeaxlr.Options{Scenario: scenario, Marker: filepath.Join(dir, "marker")})
	if err != nil {
		t.Fatal(err)
	}
	deps, err := compose.Dependencies(filepath.Join(dir, "state"), bin, "test:api")
	if err != nil {
		t.Fatal(err)
	}
	local := NewLocal(deps)
	socket := filepath.Join(dir, "cp.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- NewServer(local).Serve(ctx, socket) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
		if _, err := os.Stat(socket); err == nil {
			t.Error("socket left behind")
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for !Reachable(context.Background(), socket) {
		if time.Now().After(deadline) {
			t.Fatal("daemon not reachable")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return NewClient(socket), local, dir
}

func definition(name string, level string) application.BotDefinition {
	return application.BotDefinition{
		Tenant: "ops", Name: name, Owner: "sre", Mission: "diagnose incidents on this host",
		AllowedTools: []string{"local_read", "local_exec"}, Level: level,
		ToolRules: domain.ToolRulesSnapshot{Allow: []domain.ToolRuleSnapshot{{Tool: "local_exec", Arguments: map[string]string{"program": "journalctl"}}}},
		Model:     application.ModelPolicyDefinition{Model: "anthropic/claude-haiku-5.5"},
		Budgets:   application.BudgetsDefinition{TaskTimeoutSeconds: 60},
	}
}

func TestClientAndServerCarryTheWholeFlow(t *testing.T) {
	client, _, _ := serve(t, "completed")
	ctx := context.Background()

	bot, err := client.RegisterBot(ctx, definition("sre-host-bot", "observe"))
	if err != nil || bot.Status != "Registered" || bot.Level != "observe" || len(bot.ToolRules.Allow) != 1 {
		t.Fatalf("register: %v %+v", err, bot)
	}
	if _, err := client.RegisterBot(ctx, definition("sre-host-bot", "observe")); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("duplicate register: %v", err)
	}
	var remote RemoteError
	if _, err := client.RegisterBot(ctx, definition("Bad Name", "")); err == nil || !errorsAs(err, &remote) || remote.Kind != KindValidation {
		t.Errorf("bad definition: %v", err)
	}
	if bots, err := client.ListBots(ctx); err != nil || len(bots) != 1 {
		t.Errorf("list: %v %d", err, len(bots))
	}
	if got, err := client.GetBot(ctx, "sre-host-bot"); err != nil || got.ID != "sre-host-bot" {
		t.Errorf("get bot: %v", err)
	}
	if _, err := client.GetBot(ctx, "nobody"); !domain.IsNotFound(err) {
		t.Errorf("unknown bot: %v", err)
	}
	if _, err := client.EnableBot(ctx, "sre-host-bot"); err != nil {
		t.Fatal(err)
	}

	submitted, err := client.SubmitTask(ctx, SubmitRequest{BotID: "sre-host-bot", Objective: "Look at the host", IdempotencyKey: "k-1", CorrelationID: "c-1"})
	if err != nil || !submitted.Created || submitted.Task.Status != "Pending" || submitted.Task.CorrelationID != "c-1" {
		t.Fatalf("submit: %v %+v", err, submitted)
	}
	again, err := client.SubmitTask(ctx, SubmitRequest{BotID: "sre-host-bot", Objective: "Look at the host", IdempotencyKey: "k-1"})
	if err != nil || again.Created || again.Task.ID != submitted.Task.ID {
		t.Errorf("resubmit: %v %+v", err, again)
	}
	at := time.Date(2026, 10, 24, 12, 3, 0, 0, time.UTC)
	alert, err := client.Alert(ctx, AlertRequest{BotID: "sre-host-bot", Unit: "demo-broken", At: at, WindowSeconds: 600, UserManager: true})
	if err != nil || !alert.Created || alert.Task.IdempotencyKey != "alert:demo-broken.service:2026-10-24T12:00:00Z" || !strings.Contains(alert.Task.Objective, "--user") {
		t.Errorf("alert: %v %+v", err, alert)
	}
	if repeat, err := client.Alert(ctx, AlertRequest{BotID: "sre-host-bot", Unit: "demo-broken.service", At: at.Add(time.Minute)}); err != nil || repeat.Created {
		t.Errorf("repeated alert: %v %+v", err, repeat)
	}

	if _, err := client.GetReport(ctx, submitted.Task.ID); !domain.IsNotFound(err) {
		t.Errorf("report before the run: %v", err)
	}
	run, err := client.RunTask(ctx, submitted.Task.ID)
	if err != nil || run.Task.Status != "Completed" || run.Execution.Outcome != "Completed" || run.Execution.Policy == nil || !run.Execution.Policy.Enforced || run.Execution.Policy.RuntimeLevel != "act-within-policy" {
		t.Fatalf("run: %v %+v", err, run)
	}
	if _, err := client.RunTask(ctx, submitted.Task.ID); err == nil || !errorsAs(err, &remote) || remote.Kind != KindState {
		t.Errorf("second run: %v", err)
	}
	report, err := client.GetReport(ctx, submitted.Task.ID)
	if err != nil || !strings.HasPrefix(report.Content, "# Report") || report.Reference != run.Execution.Result.Artifacts[0].Reference || report.TaskID != submitted.Task.ID {
		t.Errorf("report: %v %+v", err, report)
	}
	if task, err := client.GetTask(ctx, submitted.Task.ID); err != nil || task.Status != "Completed" {
		t.Errorf("get task: %v %+v", err, task)
	}
	if execution, err := client.GetExecution(ctx, run.Execution.ID); err != nil || execution.Result.Answer == "" {
		t.Errorf("get execution: %v", err)
	}
	if _, err := client.GetExecution(ctx, "exec-missing"); !domain.IsNotFound(err) {
		t.Errorf("unknown execution: %v", err)
	}
	if _, err := client.GetTask(ctx, "bad id"); err == nil || !errorsAs(err, &remote) || remote.Kind != KindValidation {
		t.Errorf("bad task id: %v", err)
	}
	if _, err := client.DecideTask(ctx, submitted.Task.ID, "approve"); err == nil || remote.Kind == "" {
		t.Errorf("decide a completed task: %v", err)
	}
}

func TestDecisionGoesThroughTheRunner(t *testing.T) {
	client, _, dir := serve(t, "approval-flow")
	ctx := context.Background()
	if _, err := client.RegisterBot(ctx, definition("sre-host-fixer", "act-with-approval")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "sre-host-fixer"); err != nil {
		t.Fatal(err)
	}
	submitted, err := client.SubmitTask(ctx, SubmitRequest{BotID: "sre-host-fixer", Objective: "Restart demo-broken.service", IdempotencyKey: "fix-1"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := client.RunTask(ctx, submitted.Task.ID)
	if err != nil || run.Task.Status != "WaitingApproval" || run.Execution.Result.PendingDecision == nil {
		t.Fatalf("run: %v %+v", err, run)
	}
	if _, err := os.Stat(filepath.Join(dir, "marker")); err == nil {
		t.Fatal("the write ran before the approval")
	}
	if _, err := client.DecideTask(ctx, submitted.Task.ID, "maybe"); err == nil {
		t.Error("bad decision accepted")
	}
	decided, err := client.DecideTask(ctx, submitted.Task.ID, "approve")
	if err != nil || decided.Task.Status != "Completed" || len(decided.Task.ExecutionIDs) != 2 {
		t.Fatalf("decide: %v %+v", err, decided)
	}
	if _, err := os.Stat(filepath.Join(dir, "marker")); err != nil {
		t.Error("the write did not run after the approval")
	}
}

func TestServerRefusesBadBodies(t *testing.T) {
	deps, _ := compose.Dependencies(t.TempDir(), "axlr-tui", "test")
	handler := NewServer(NewLocal(deps)).Handler()
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/v1/bots", `{"nope": 1}`},
		{http.MethodPost, "/v1/tasks", `not json`},
		{http.MethodPost, "/v1/alerts", `{"unit": 3}`},
		{http.MethodPost, "/v1/tasks/task-1/decide", `{"decision": ["x"]}`},
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
		var failure ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &failure)
		if rec.Code != http.StatusBadRequest || failure.Kind != KindValidation {
			t.Errorf("%s %s: %d %s", tc.method, tc.path, rec.Code, rec.Body)
		}
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("healthz = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tasks/task-404", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing task = %d", rec.Code)
	}
}

func TestErrorsRoundTrip(t *testing.T) {
	cases := []struct {
		err    error
		status int
		kind   string
	}{
		{domain.NotFoundError{Kind: "task", ID: "t"}, http.StatusNotFound, KindNotFound},
		{domain.ValidationError{Field: "f", Reason: "r"}, http.StatusBadRequest, KindValidation},
		{domain.StateError{Subject: "s", Current: "c", Attempted: "a"}, http.StatusConflict, KindState},
		{domain.ConflictError{Kind: "bot", ID: "b"}, http.StatusConflict, KindConflict},
		{context.Canceled, http.StatusInternalServerError, KindInternal},
	}
	for _, tc := range cases {
		status, response := toWire(tc.err)
		if status != tc.status || response.Kind != tc.kind || response.Message != tc.err.Error() {
			t.Errorf("%v: %d %+v", tc.err, status, response)
		}
		back := fromWire(status, response)
		if tc.kind == KindNotFound && !domain.IsNotFound(back) {
			t.Errorf("not found did not come back: %v", back)
		}
		if back.Error() != tc.err.Error() {
			t.Errorf("message changed: %v", back)
		}
	}
	if err := fromWire(502, ErrorResponse{}); !strings.Contains(err.Error(), "HTTP 502") {
		t.Errorf("empty error = %v", err)
	}
}

func TestServeErrorsAndReachability(t *testing.T) {
	deps, _ := compose.Dependencies(t.TempDir(), "axlr-tui", "test")
	server := NewServer(NewLocal(deps))
	dir := t.TempDir()
	if !strings.Contains(dir, "/") {
		t.Fatal("odd temp dir")
	}
	// A stale socket file is replaced.
	stale := filepath.Join(dir, "stale.sock")
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, stale) }()
	deadline := time.Now().Add(5 * time.Second)
	for !Reachable(context.Background(), stale) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !Reachable(context.Background(), stale) {
		t.Fatal("not reachable over the replaced socket")
	}
	cancel()
	if err := <-done; err != nil {
		t.Error(err)
	}
	if Reachable(context.Background(), stale) {
		t.Error("reachable after shutdown")
	}
	// A socket under a file cannot be made.
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := server.Serve(context.Background(), filepath.Join(file, "x.sock")); err == nil {
		t.Error("socket under a file served")
	}
	// A stale path that is a non-empty directory cannot be removed.
	full := filepath.Join(dir, "full")
	if err := os.MkdirAll(filepath.Join(full, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := server.Serve(context.Background(), full); err == nil || !strings.Contains(err.Error(), "stale socket") {
		t.Errorf("non-empty directory: %v", err)
	}
	if Reachable(context.Background(), filepath.Join(dir, "nothing.sock")) {
		t.Error("reachable without a daemon")
	}
	client := NewClient(filepath.Join(dir, "nothing.sock"))
	if _, err := client.ListBots(context.Background()); err == nil || !strings.Contains(err.Error(), "socket") {
		t.Errorf("client without a daemon: %v", err)
	}
}

func TestLocalReportReference(t *testing.T) {
	dir := t.TempDir()
	bin, _, _ := fakeaxlr.Write(dir, fakeaxlr.Options{Scenario: "completed"})
	deps, _ := compose.Dependencies(filepath.Join(dir, "state"), bin, "test")
	local := NewLocal(deps)
	ctx := context.Background()
	if _, err := local.RegisterBot(ctx, definition("b", "")); err != nil {
		t.Fatal(err)
	}
	if _, err := local.EnableBot(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	if _, err := local.GetReport(ctx, "bad id"); err == nil {
		t.Error("bad id accepted")
	}
	if _, err := local.GetReport(ctx, "task-404"); !domain.IsNotFound(err) {
		t.Errorf("unknown task: %v", err)
	}
	submitted, _ := local.SubmitTask(ctx, SubmitRequest{BotID: "b", Objective: "o", IdempotencyKey: "k"})
	if _, err := local.GetReport(ctx, submitted.Task.ID); !domain.IsNotFound(err) {
		t.Errorf("no report yet: %v", err)
	}
	run, err := local.RunTask(ctx, submitted.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	report, err := local.GetReport(ctx, submitted.Task.ID)
	if err != nil || report.Reference != run.Execution.Result.Artifacts[0].Reference {
		t.Errorf("report = %+v %v", report, err)
	}
	if local.Runner() == nil {
		t.Error("no runner")
	}
	for _, bad := range []func() error{
		func() error { _, err := local.EnableBot(ctx, "Bad"); return err },
		func() error { _, err := local.GetBot(ctx, "Bad"); return err },
		func() error { _, err := local.RunTask(ctx, "bad id"); return err },
		func() error { _, err := local.DecideTask(ctx, "bad id", "approve"); return err },
		func() error { _, err := local.GetTask(ctx, "bad id"); return err },
		func() error { _, err := local.GetExecution(ctx, "bad id"); return err },
		func() error { _, err := local.Alert(ctx, AlertRequest{BotID: "b"}); return err },
	} {
		if err := bad(); err == nil {
			t.Error("bad id accepted")
		}
	}
}
