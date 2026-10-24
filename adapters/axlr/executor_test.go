package axlr

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
	_ application.RuntimeExecutor  = (*Executor)(nil)
	_ application.DecisionExecutor = (*Executor)(nil)
	_ application.TraceLocator     = (*Executor)(nil)
)

func fake(t *testing.T, scenario string) (*Executor, string) {
	t.Helper()
	state := t.TempDir()
	argsFile := filepath.Join(state, "args.txt")
	t.Setenv("FAKE_AXLR", scenario)
	t.Setenv("FAKE_AXLR_ARGS", argsFile)
	e := NewExecutor(os.Args[0], state)
	e.Args = []string{"-test.run=^TestHelperProcess$", "--"}
	e.WaitDelay = 200 * time.Millisecond
	return e, argsFile
}

func request(timeout time.Duration) application.ExecutionRequest {
	budgets, _ := domain.NewBudgets(20, 60, 2, 900*time.Second)
	return application.ExecutionRequest{
		ExecutionID: "exec-1", TaskID: "task-1", BotID: "sii", TenantID: "engineering",
		Objective: "List the files in your workspace", Model: "anthropic/claude-haiku-5.5",
		AllowedTools: []string{"observability.query_logs"}, Budgets: budgets, Timeout: timeout,
	}
}

func readArgs(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(string(raw), "\n")
}

func TestExecuteReadsTheResultObject(t *testing.T) {
	e, argsFile := fake(t, "result-completed")
	e.Mode = "research"
	report, err := e.Execute(context.Background(), request(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != domain.ExecutionCompleted || report.FailureReason != "" {
		t.Fatalf("outcome %s reason %q", report.Outcome, report.FailureReason)
	}
	r := report.Result
	if r.Answer != "The spike came from pod api-3." || r.SessionID != "sess-1" || len(r.Sessions) != 2 || r.Sessions[0] != "sess-0" {
		t.Errorf("result = %+v", r)
	}
	if len(r.ToolCalls) != 1 || r.ToolCalls[0].Name != "local_read" || r.ToolCalls[0].Status != "completed" || r.ToolCalls[0].Decision != "auto_approve" {
		t.Errorf("tool calls = %+v", r.ToolCalls)
	}
	if len(r.Artifacts) != 1 || r.Artifacts[0].Name != "report.md" || r.Artifacts[0].MediaType != "text/markdown" || r.Artifacts[0].Reference != "artifacts/report.md" {
		t.Errorf("artifacts = %+v", r.Artifacts)
	}
	u := report.Usage
	if u.Requests != 2 || u.PromptTokens != 3120 || u.CachedTokens != 2048 || u.CompletionTokens != 96 || u.ReasoningTokens != 40 || u.ToolCalls != 1 || u.CostUSD != 0.0021 {
		t.Errorf("usage = %+v", u)
	}
	// The trace keeps every raw line; stderr goes beside it.
	trace, err := os.ReadFile(report.TracePath)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(trace), "\n"); lines != 7 {
		t.Errorf("trace has %d lines: %s", lines, trace)
	}
	if _, err := os.Stat(strings.TrimSuffix(report.TracePath, ".jsonl") + ".stderr.log"); err != nil {
		t.Error(err)
	}
	args := readArgs(t, argsFile)
	want := []string{"--exec", "List the files in your workspace", "--json", "--approve", "none", "--model", "anthropic/claude-haiku-5.5", "--mode", "research", "--policy", filepath.Join(e.StateDir, "policies", "exec-1.json"), "--root", filepath.Join(e.StateDir, "workspaces", "sii")}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("args = %q\nwant %q", args, want)
	}
	if _, err := os.Stat(filepath.Join(e.StateDir, "workspaces", "sii")); err != nil {
		t.Error("workspace not created")
	}
}

func TestExecuteWithoutModelOmitsTheFlag(t *testing.T) {
	e, argsFile := fake(t, "legacy-completed")
	e.Approve = "local"
	req := request(0)
	req.Model = ""
	if _, err := e.Execute(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(readArgs(t, argsFile), " ")
	if strings.Contains(args, "--model") || !strings.Contains(args, "--approve local") || strings.Contains(args, "--mode") {
		t.Errorf("args = %s", args)
	}
}

func TestExecuteOutcomes(t *testing.T) {
	cases := []struct {
		scenario string
		outcome  domain.ExecutionOutcome
		reason   string
		answer   string
		session  string
		pending  string
		calls    int
	}{
		{"result-needs-decision", domain.ExecutionDecisionRequired, "", "I will run it now.", "sess-1", "c2", 1},
		{"result-needs-decision-no-pending", domain.ExecutionFailed, "runtime result says needs_decision but names no pending call", "", "sess-1", "", 0},
		{"result-failed", domain.ExecutionFailed, "provider refused", "", "sess-1", "", 0},
		{"result-failed-no-error", domain.ExecutionFailed, "runtime reported failed", "", "sess-1", "", 0},
		{"result-interrupted", domain.ExecutionFailed, "interrupted: cancelled", "", "sess-1", "", 0},
		{"result-interrupted-no-error", domain.ExecutionFailed, "interrupted: the turn stopped before its end", "", "sess-1", "", 0},
		{"result-unknown-status", domain.ExecutionFailed, `runtime result has unknown status "meh"`, "", "sess-1", "", 0},
		{"result-bad-schema", domain.ExecutionCompleted, "", "The spike ", "sess-1", "", 0},
		{"legacy-completed", domain.ExecutionCompleted, "", "The spike came from pod api-3.", "sess-1", "", 1},
		{"legacy-continued", domain.ExecutionCompleted, "", "The spike ", "sess-2", "", 0},
		{"legacy-decision", domain.ExecutionDecisionRequired, "", "", "sess-1", "c2", 1},
		{"legacy-decision-missing", domain.ExecutionFailed, "runtime exited 3 (decision required) but reported no pending call", "", "sess-1", "", 0},
		{"legacy-failed", domain.ExecutionFailed, "the prompt kept reaching the context cap", "", "sess-1", "", 0},
		{"no-json", domain.ExecutionFailed, "exit code 1: axlr-tui: no model provider: set OPENROUTER_API_KEY", "", "", "", 0},
		{"exit-7", domain.ExecutionFailed, "runtime exited with code 7", "", "", "", 0},
		{"garbage", domain.ExecutionCompleted, "", "The spike ", "", "", 3},
	}
	for _, c := range cases {
		t.Run(c.scenario, func(t *testing.T) {
			e, _ := fake(t, c.scenario)
			report, err := e.Execute(context.Background(), request(time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			if report.Outcome != c.outcome || report.FailureReason != c.reason {
				t.Errorf("outcome %s reason %q", report.Outcome, report.FailureReason)
			}
			if report.Result.Answer != c.answer || report.Result.SessionID != c.session || len(report.Result.ToolCalls) != c.calls || report.Usage.ToolCalls != c.calls {
				t.Errorf("result = %+v usage = %+v", report.Result, report.Usage)
			}
			pending := report.Result.PendingDecision
			if (c.pending == "" && pending != nil) || (c.pending != "" && (pending == nil || pending.CallID != c.pending || pending.SessionID != c.session)) {
				t.Errorf("pending = %+v", pending)
			}
			if c.scenario == "result-needs-decision" && string(pending.Arguments) != `{"program":"touch"}` {
				t.Errorf("pending arguments = %s", pending.Arguments)
			}
			if c.scenario == "legacy-decision" && pending.Arguments != nil {
				t.Errorf("legacy pending has arguments: %s", pending.Arguments)
			}
			if report.TracePath == "" {
				t.Error("no trace path")
			}
		})
	}
}

func TestExecuteSessionsFromEvents(t *testing.T) {
	e, _ := fake(t, "legacy-continued")
	report, _ := e.Execute(context.Background(), request(time.Minute))
	if len(report.Result.Sessions) != 2 || report.Result.Sessions[1] != "sess-2" {
		t.Errorf("sessions = %v", report.Result.Sessions)
	}
}

func TestExecuteTimesOutAndKeepsThePartialTrace(t *testing.T) {
	e, _ := fake(t, "hang")
	start := time.Now()
	report, err := e.Execute(context.Background(), request(300*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 10*time.Second {
		t.Error("the process was not stopped")
	}
	if report.Outcome != domain.ExecutionFailed || report.FailureReason != "timed out after 300ms" {
		t.Errorf("outcome %s reason %q", report.Outcome, report.FailureReason)
	}
	if report.Result.Answer != "The spike " || report.Result.SessionID != "sess-1" {
		t.Errorf("partial result lost: %+v", report.Result)
	}
	trace, _ := os.ReadFile(report.TracePath)
	if !strings.Contains(string(trace), `"session":"sess-1"`) {
		t.Errorf("trace = %s", trace)
	}
}

func TestExecuteUsesTheDefaultTimeout(t *testing.T) {
	e, _ := fake(t, "hang")
	e.DefaultTimeout = 300 * time.Millisecond
	report, err := e.Execute(context.Background(), request(0))
	if err != nil || report.FailureReason != "timed out after 300ms" {
		t.Errorf("%v %q", err, report.FailureReason)
	}
	zero := &Executor{Binary: "x"}
	if zero.timeout(0) != defaultTimeout || zero.waitDelay() != defaultWaitDelay || zero.approve() != "none" {
		t.Error("zero-value defaults wrong")
	}
	if (&Executor{}).binary() != "axlr-tui" || NewExecutor("", "s").Binary != "axlr-tui" {
		t.Error("default binary wrong")
	}
}

func TestExecuteCancelled(t *testing.T) {
	e, _ := fake(t, "hang")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	report, err := e.Execute(ctx, request(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != domain.ExecutionFailed || !strings.HasPrefix(report.FailureReason, "cancelled: ") {
		t.Errorf("outcome %s reason %q", report.Outcome, report.FailureReason)
	}
}

func TestExecuteRejectsOversizedLines(t *testing.T) {
	e, _ := fake(t, "huge-line")
	report, err := e.Execute(context.Background(), request(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != domain.ExecutionFailed || !strings.HasPrefix(report.FailureReason, "reading runtime output: ") {
		t.Errorf("outcome %s reason %q", report.Outcome, report.FailureReason)
	}
}

func TestExecuteRefusals(t *testing.T) {
	e, _ := fake(t, "result-completed")
	ctx := context.Background()
	if _, err := e.Execute(ctx, application.ExecutionRequest{}); err == nil {
		t.Error("empty request accepted")
	}
	req := request(time.Minute)
	req.ExecutionID = ""
	if _, err := e.Execute(ctx, req); err == nil {
		t.Error("request without execution id accepted")
	}
	missing := NewExecutor(filepath.Join(t.TempDir(), "no-such-axlr-tui"), t.TempDir())
	if _, err := missing.Execute(ctx, request(time.Minute)); err == nil || !strings.Contains(err.Error(), "start") {
		t.Errorf("missing binary: %v", err)
	}
	blocked := NewExecutor(os.Args[0], filepath.Join(t.TempDir(), "file"))
	_ = os.WriteFile(blocked.StateDir, []byte("x"), 0o644)
	if _, err := blocked.Execute(ctx, request(time.Minute)); err == nil {
		t.Error("state dir under a file accepted")
	}
	traceBlocked, _ := fake(t, "result-completed")
	_ = os.MkdirAll(filepath.Join(traceBlocked.StateDir, "traces", "exec-1.jsonl"), 0o755)
	if _, err := traceBlocked.Execute(ctx, request(time.Minute)); err == nil {
		t.Error("trace path that is a directory accepted")
	}
	stderrBlocked, _ := fake(t, "result-completed")
	_ = os.MkdirAll(filepath.Join(stderrBlocked.StateDir, "traces", "exec-1.stderr.log"), 0o755)
	if _, err := stderrBlocked.Execute(ctx, request(time.Minute)); err == nil {
		t.Error("stderr path that is a directory accepted")
	}
}

func TestDecide(t *testing.T) {
	e, argsFile := fake(t, "result-completed")
	report, err := e.Decide(context.Background(), application.DecisionRequest{ExecutionID: "exec-2", BotID: "sii", SessionID: "sess-1", CallID: "c2", Decision: "approve", Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != domain.ExecutionCompleted || !strings.HasSuffix(report.TracePath, "exec-2.jsonl") {
		t.Errorf("report = %+v", report)
	}
	args := strings.Join(readArgs(t, argsFile), " ")
	if !strings.HasPrefix(args, "--session sess-1 --decide approve --json --approve none --call c2 --policy ") || !strings.Contains(args, filepath.Join("policies", "exec-2.json")+" --root ") {
		t.Errorf("args = %s", args)
	}
	if report.Policy == nil || !report.Policy.Enforced || report.Policy.Level != "observe" || report.Policy.RuntimeLevel != "observe" {
		t.Errorf("policy = %+v", report.Policy)
	}
	e2, argsFile2 := fake(t, "result-completed")
	if _, err := e2.Decide(context.Background(), application.DecisionRequest{ExecutionID: "exec-3", BotID: "sii", SessionID: "sess-1", Decision: "deny"}); err != nil {
		t.Fatal(err)
	}
	if args := strings.Join(readArgs(t, argsFile2), " "); strings.Contains(args, "--call") {
		t.Errorf("args = %s", args)
	}
	for _, bad := range []application.DecisionRequest{{Decision: "approve"}, {SessionID: "s", Decision: "maybe"}} {
		if _, err := e.Decide(context.Background(), bad); err == nil {
			t.Errorf("bad decision %+v accepted", bad)
		}
	}
}

func TestTailWriter(t *testing.T) {
	w := &tailWriter{limit: 8}
	_, _ = w.Write([]byte("abcdefgh\n"))
	_, _ = w.Write([]byte("ijk\n\n  "))
	if got := w.lastLine(); got != "ijk" {
		t.Errorf("lastLine = %q", got)
	}
	if (&tailWriter{limit: 8}).lastLine() != "" {
		t.Error("empty tail should have no line")
	}
}

// TestLiveExec runs the real axlr-tui once, only when AXLR_LIVE=1. It
// needs a binary with --exec (UNDERPASS_AXLR_BIN, or axlr-tui on PATH)
// and a model provider configured for it (OPENROUTER_API_KEY or
// ~/.config/axlr/credentials.json).
func TestLiveExec(t *testing.T) {
	if os.Getenv("AXLR_LIVE") != "1" {
		t.Skip("set AXLR_LIVE=1 to run axlr-tui for real")
	}
	e := NewExecutor(os.Getenv("UNDERPASS_AXLR_BIN"), t.TempDir())
	req := application.ExecutionRequest{
		ExecutionID: "exec-live", TaskID: "task-live", BotID: "live-bot", TenantID: "test",
		Objective: "Reply with exactly the word pong and nothing else.", Model: os.Getenv("UNDERPASS_AXLR_MODEL"), Timeout: 3 * time.Minute,
	}
	report, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("outcome %s reason %q answer %q session %s usage %+v trace %s", report.Outcome, report.FailureReason, report.Result.Answer, report.Result.SessionID, report.Usage, report.TracePath)
	if report.Outcome != domain.ExecutionCompleted {
		t.Fatalf("outcome %s: %s", report.Outcome, report.FailureReason)
	}
	if !strings.Contains(strings.ToLower(report.Result.Answer), "pong") || report.Result.SessionID == "" {
		t.Errorf("answer %q session %q", report.Result.Answer, report.Result.SessionID)
	}
}

func TestTracePathReportsOnlyAKeptTrace(t *testing.T) {
	e := NewExecutor("axlr-tui", t.TempDir())
	if _, ok := e.TracePath("exec-1"); ok {
		t.Error("a trace that was never written was found")
	}
	_ = os.MkdirAll(filepath.Join(e.StateDir, "traces", "exec-dir.jsonl"), 0o755)
	if _, ok := e.TracePath("exec-dir"); ok {
		t.Error("a directory passed as a trace")
	}
	want := filepath.Join(e.StateDir, "traces", "exec-1.jsonl")
	_ = os.WriteFile(want, []byte("{}\n"), 0o644)
	if got, ok := e.TracePath("exec-1"); !ok || got != want {
		t.Errorf("TracePath = %q %v, want %q", got, ok, want)
	}
}
