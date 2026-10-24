package unixapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
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

	bot, err := client.RegisterBot(ctx, definition("bot-a", "observe"))
	if err != nil || bot.Status != "Registered" || bot.Level != "observe" || len(bot.ToolRules.Allow) != 1 {
		t.Fatalf("register: %v %+v", err, bot)
	}
	if _, err := client.RegisterBot(ctx, definition("bot-a", "observe")); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("duplicate register: %v", err)
	}
	var remote RemoteError
	if _, err := client.RegisterBot(ctx, definition("Bad Name", "")); err == nil || !errorsAs(err, &remote) || remote.Kind != KindValidation {
		t.Errorf("bad definition: %v", err)
	}
	if bots, err := client.ListBots(ctx); err != nil || len(bots) != 1 {
		t.Errorf("list: %v %d", err, len(bots))
	}
	if got, err := client.GetBot(ctx, "bot-a"); err != nil || got.ID != "bot-a" {
		t.Errorf("get bot: %v", err)
	}
	if _, err := client.GetBot(ctx, "nobody"); !domain.IsNotFound(err) {
		t.Errorf("unknown bot: %v", err)
	}
	if _, err := client.EnableBot(ctx, "bot-a"); err != nil {
		t.Fatal(err)
	}

	submitted, err := client.SubmitTask(ctx, SubmitRequest{BotID: "bot-a", Objective: "Look at the host", IdempotencyKey: "k-1", CorrelationID: "c-1"})
	if err != nil || !submitted.Created || submitted.Task.Status != "Pending" || submitted.Task.CorrelationID != "c-1" {
		t.Fatalf("submit: %v %+v", err, submitted)
	}
	again, err := client.SubmitTask(ctx, SubmitRequest{BotID: "bot-a", Objective: "Look at the host", IdempotencyKey: "k-1"})
	if err != nil || again.Created || again.Task.ID != submitted.Task.ID {
		t.Errorf("resubmit: %v %+v", err, again)
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
	if _, err := client.RegisterBot(ctx, definition("bot-b", "act-with-approval")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "bot-b"); err != nil {
		t.Fatal(err)
	}
	submitted, err := client.SubmitTask(ctx, SubmitRequest{BotID: "bot-b", Objective: "Restart demo-broken.service", IdempotencyKey: "fix-1"})
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
		{http.MethodPost, "/v1/nodes", `{"id": 3}`},
		{http.MethodPost, "/v1/nodes/n/executions/exec-1/report", `{"outcome": 1}`},
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
	if _, err := client.ListBots(context.Background()); err == nil || !strings.Contains(err.Error(), "control plane at") {
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
		func() error { _, err := local.GetNode(ctx, "Bad"); return err },
	} {
		if err := bad(); err == nil {
			t.Error("bad id accepted")
		}
	}
}

func TestNodeProtocolOverTheSocket(t *testing.T) {
	client, _, _ := serve(t, "completed")
	ctx := context.Background()
	if nodes, err := client.ListNodes(ctx); err != nil || len(nodes) != 0 {
		t.Errorf("no nodes yet: %v %d", err, len(nodes))
	}
	node, err := client.Register(ctx, application.NodeRegistration{ID: "web-1", HostName: "web-1.example", Labels: map[string]string{"host": "web-1"}, Capabilities: domain.NodeCapabilities{AXLRVersion: "dev", PolicySupported: true}})
	if err != nil || node.Status != "Online" || node.Capabilities.AXLRVersion != "dev" {
		t.Fatalf("register: %v %+v", err, node)
	}
	if _, err := client.Register(ctx, application.NodeRegistration{ID: "Bad Id", HostName: "h"}); err == nil {
		t.Error("bad registration accepted")
	}
	if beat, err := client.Heartbeat(ctx, "web-1"); err != nil || beat.Status != "Online" {
		t.Errorf("heartbeat: %v %+v", err, beat)
	}
	if _, err := client.Heartbeat(ctx, "ghost"); !domain.IsNotFound(err) {
		t.Errorf("unknown heartbeat: %v", err)
	}
	if got, err := client.GetNode(ctx, "web-1"); err != nil || got.HostName != "web-1.example" {
		t.Errorf("get node: %v %+v", err, got)
	}
	if nodes, err := client.ListNodes(ctx); err != nil || len(nodes) != 1 {
		t.Errorf("list nodes: %v %d", err, len(nodes))
	}
	// Nothing to do: the long poll waits and answers 204.
	started := time.Now()
	if _, found, err := client.NextAssignment(ctx, "web-1", 300*time.Millisecond); err != nil || found {
		t.Errorf("empty poll: %v %v", err, found)
	}
	if time.Since(started) < 250*time.Millisecond {
		t.Error("the long poll did not wait")
	}
	if _, err := client.RegisterBot(ctx, definition("bot-a", "observe")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "bot-a"); err != nil {
		t.Fatal(err)
	}
	submitted, err := client.SubmitTask(ctx, SubmitRequest{BotID: "bot-a", Objective: "List the files in your workspace", IdempotencyKey: "k-1"})
	if err != nil {
		t.Fatal(err)
	}
	assignment, found, err := client.NextAssignment(ctx, "web-1", 5*time.Second)
	if err != nil || !found || assignment.Run == nil || assignment.Run.TaskID != domain.TaskID(submitted.Task.ID) || assignment.Run.Objective != "List the files in your workspace" || assignment.Kind != domain.ExecutionRun {
		t.Fatalf("assignment: %v %v %+v", err, found, assignment)
	}
	if task, _ := client.GetTask(ctx, submitted.Task.ID); task.Status != "Running" {
		t.Errorf("task = %+v", task)
	}
	if execution, _ := client.GetExecution(ctx, assignment.ExecutionID.String()); execution.NodeID != "web-1" || execution.LeaseUntil == nil {
		t.Errorf("execution = %+v", execution)
	}
	until, err := client.RenewLease(ctx, "web-1", assignment.ExecutionID)
	if err != nil || until.Before(time.Now()) {
		t.Errorf("renew: %v %s", err, until)
	}
	if _, err := client.RenewLease(ctx, "db-1", assignment.ExecutionID); err == nil {
		t.Error("another node renewed the lease")
	}
	report := application.ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "3 files", SessionID: "sess-1"}, Usage: domain.Usage{Requests: 1}, TracePath: "node:web-1:traces/exec.jsonl", Policy: &domain.PolicyRecord{Level: "observe", RuntimeLevel: "observe", Enforced: true}}
	ack, err := client.Report(ctx, "web-1", assignment.ExecutionID, report)
	if err != nil || !ack.Accepted || ack.Outcome != domain.ExecutionCompleted {
		t.Fatalf("report: %v %+v", err, ack)
	}
	again, err := client.Report(ctx, "web-1", assignment.ExecutionID, report)
	if err != nil || again.Accepted || again.Outcome != domain.ExecutionCompleted {
		t.Errorf("second report: %v %+v", err, again)
	}
	task, err := client.GetTask(ctx, submitted.Task.ID)
	if err != nil || task.Status != "Completed" {
		t.Errorf("task after report = %+v", task)
	}
	got, err := client.GetReport(ctx, submitted.Task.ID)
	if err != nil || got.Content != "3 files" {
		t.Errorf("report content = %+v %v", got, err)
	}
	if execution, _ := client.GetExecution(ctx, assignment.ExecutionID.String()); execution.TracePath != report.TracePath || execution.Policy == nil || !execution.Policy.Enforced {
		t.Errorf("execution = %+v", execution)
	}
	if _, err := client.Report(ctx, "web-1", "exec-missing", report); !domain.IsNotFound(err) {
		t.Errorf("unknown execution: %v", err)
	}
	if _, err := client.Report(ctx, "web-1", assignment.ExecutionID, application.ExecutionReport{Outcome: domain.ExecutionRunning}); err == nil {
		t.Error("a non-final report was accepted")
	}
	if _, _, err := client.NextAssignment(ctx, "Bad Id", 0); err == nil {
		t.Error("bad node id accepted")
	}
	if _, _, err := client.NextAssignment(ctx, "ghost", 0); !domain.IsNotFound(err) {
		t.Errorf("unknown node: %v", err)
	}
}

func TestServeTCPAndNextAssignmentValidation(t *testing.T) {
	deps, _ := compose.Dependencies(t.TempDir(), "axlr-tui", "test")
	server := NewServer(NewLocal(deps))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.ServeTCP(ctx, address) }()
	client := NewClient("http://" + address)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := client.ListNodes(context.Background()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("tcp server not reachable")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := client.Register(context.Background(), application.NodeRegistration{ID: "web-1", HostName: "h"}); err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/nodes/web-1/assignments/next?wait=soon", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad wait = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/nodes/web-1/executions/Bad%20Id/lease", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad execution id = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/nodes/Bad%20Id/heartbeat", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad node id = %d", rec.Code)
	}
	// A poll whose request is cancelled answers 204 at once.
	pollCtx, cancelPoll := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancelPoll() }()
	if _, found, err := client.NextAssignment(pollCtx, "web-1", 10*time.Second); found || (err != nil && !errors.Is(err, context.Canceled)) {
		t.Logf("cancelled poll: %v %v", err, found)
	}
	cancel()
	if err := <-done; err != nil {
		t.Error(err)
	}
	if err := server.ServeTCP(context.Background(), "256.256.256.256:1"); err == nil {
		t.Error("bad address served")
	}
	if NewClient("unix:///tmp/nothing.sock").address != "/tmp/nothing.sock" {
		t.Error("unix:// prefix not stripped")
	}
}
