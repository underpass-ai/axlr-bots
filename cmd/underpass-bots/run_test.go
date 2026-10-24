package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/underpass-bots/adapters/filestore"
	"github.com/underpass-ai/underpass-bots/domain"
)

// fakeAXLR writes a shell stand-in for axlr-tui that prints the lines of
// the scenario and exits with its code.
func fakeAXLR(t *testing.T, scenario string) string {
	t.Helper()
	path, _ := recordingFakeAXLR(t, scenario)
	return path
}

// recordingFakeAXLR is fakeAXLR plus a file where the fake writes the
// arguments it was called with, one per line.
func recordingFakeAXLR(t *testing.T, scenario string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	var body string
	switch scenario {
	case "completed":
		body = `printf '%s\n' '{"type":"session","session":"sess-1"}' '{"type":"text","text":"pong"}' '{"type":"completed","session":"sess-1"}' '{"type":"result","session":"sess-1","result":{"schema":"axlr.execution-result/v1","status":"completed","session_id":"sess-1","sessions":["sess-1"],"answer":"pong","tool_calls":[],"usage":{"requests":1,"prompt_tokens":10,"completion_tokens":1,"cost_usd":0.001},"artifacts":[]}}'
exit 0`
	case "decision":
		body = `printf '%s\n' '{"type":"session","session":"sess-1"}' '{"type":"decision.required","session":"sess-1","tool":"local_exec","call_id":"c1","status":"approval"}'
exit 3`
	case "failed":
		body = `echo "axlr-tui: no model provider" >&2
exit 1`
	}
	path := filepath.Join(dir, "fake-axlr-tui")
	script := "#!/bin/sh\nfor a in \"$@\"; do if [ \"$a\" = --help ]; then echo '  -policy string' >&2; exit 0; fi; done\nprintf '%s\\n' \"$@\" > '" + argsFile + "'\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, argsFile
}

func definitionFile(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "examples", "server-incident-investigator.json")
}

type cli struct {
	t      *testing.T
	state  string
	axlr   string
	socket string
}

func (c cli) run(args ...string) (int, string, string) {
	c.t.Helper()
	var stdout, stderr bytes.Buffer
	global := []string{"--state", c.state, "--axlr-bin", c.axlr, "--actor", "test:cli"}
	if c.socket != "" {
		global = append(global, "--socket", c.socket)
	}
	code := run(append(global, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func (c cli) json(args ...string) (int, map[string]any) {
	c.t.Helper()
	code, out, errText := c.run(args...)
	var decoded map[string]any
	if out != "" {
		if err := json.Unmarshal([]byte(out), &decoded); err != nil {
			c.t.Fatalf("stdout is not JSON: %s (%v) stderr: %s", out, err, errText)
		}
	}
	return code, decoded
}

func TestPhaseZeroDemo(t *testing.T) {
	state := t.TempDir()
	c := cli{t: t, state: state, axlr: fakeAXLR(t, "completed")}

	// Register, then "restart": every call is a new process over the same state.
	code, bot := c.json("bot", "register", "--file", definitionFile(t))
	if code != exitOK || bot["status"] != "Registered" || bot["id"] != "server-incident-investigator" {
		t.Fatalf("register: %d %v", code, bot)
	}
	code, bot = c.json("bot", "enable", "server-incident-investigator")
	if code != exitOK || bot["status"] != "Enabled" {
		t.Fatalf("enable: %d %v", code, bot)
	}
	code, out, _ := c.run("bot", "list")
	if code != exitOK || !strings.Contains(out, `"server-incident-investigator"`) {
		t.Fatalf("list: %d %s", code, out)
	}
	code, bot = c.json("bot", "get", "server-incident-investigator")
	if code != exitOK || bot["status"] != "Enabled" {
		t.Fatalf("get after restart: %d %v", code, bot)
	}

	code, submitted := c.json("task", "submit", "server-incident-investigator", "--objective", "Investigate an HTTP 500 spike", "--key", "alert-1", "--correlation", "trace-abc")
	if code != exitOK || submitted["created"] != true {
		t.Fatalf("submit: %d %v", code, submitted)
	}
	task := submitted["task"].(map[string]any)
	taskID := task["id"].(string)
	if task["status"] != "Pending" || task["correlationId"] != "trace-abc" {
		t.Errorf("task = %v", task)
	}

	code, ran := c.json("task", "run", taskID)
	if code != exitOK {
		t.Fatalf("run: %d %v", code, ran)
	}
	execution := ran["execution"].(map[string]any)
	if ran["task"].(map[string]any)["status"] != "Completed" || execution["outcome"] != "Completed" {
		t.Errorf("run = %v", ran)
	}
	executionID := execution["id"].(string)
	code, got := c.json("execution", "get", executionID)
	if code != exitOK || got["result"].(map[string]any)["answer"] != "pong" || got["usage"].(map[string]any)["costUsd"] != 0.001 {
		t.Errorf("execution get: %d %v", code, got)
	}
	if _, err := os.Stat(got["tracePath"].(string)); err != nil {
		t.Error(err)
	}
	code, got = c.json("task", "get", taskID)
	if code != exitOK || got["status"] != "Completed" {
		t.Errorf("task get: %d %v", code, got)
	}

	// Resubmitting the same key names the same task; it does not run again.
	code, again := c.json("task", "submit", "server-incident-investigator", "--objective", "Investigate an HTTP 500 spike", "--key", "alert-1")
	if code != exitOK || again["created"] != false || again["task"].(map[string]any)["id"] != taskID {
		t.Errorf("resubmit: %d %v", code, again)
	}
	code, _, errText := c.run("task", "run", taskID)
	if code != exitError || !strings.Contains(errText, "is Completed: cannot start") {
		t.Errorf("second run: %d %s", code, errText)
	}
	entries, _ := os.ReadDir(filepath.Join(state, "executions"))
	if len(entries) != 1 {
		t.Errorf("%d executions, want 1", len(entries))
	}
	events, _ := os.ReadFile(filepath.Join(state, "events.jsonl"))
	if n := strings.Count(string(events), "\n"); n != 5 {
		t.Errorf("%d events: %s", n, events)
	}
}

func TestTaskRunExitCodesFollowTheOutcome(t *testing.T) {
	for scenario, want := range map[string]int{"decision": exitDecisionRequired, "failed": exitError} {
		c := cli{t: t, state: t.TempDir(), axlr: fakeAXLR(t, scenario)}
		_, _ = c.json("bot", "register", "--file", definitionFile(t))
		_, _ = c.json("bot", "enable", "server-incident-investigator")
		_, submitted := c.json("task", "submit", "server-incident-investigator", "--objective", "o", "--key", "k")
		taskID := submitted["task"].(map[string]any)["id"].(string)
		code, ran := c.json("task", "run", taskID)
		if code != want {
			t.Errorf("%s: exit %d, want %d (%v)", scenario, code, want, ran)
		}
	}
}

func TestTaskDecideContinuesAWaitingTask(t *testing.T) {
	state := t.TempDir()
	stopped := cli{t: t, state: state, axlr: fakeAXLR(t, "decision")}
	_, _ = stopped.json("bot", "register", "--file", definitionFile(t))
	_, _ = stopped.json("bot", "enable", "server-incident-investigator")
	_, submitted := stopped.json("task", "submit", "server-incident-investigator", "--objective", "Investigate an HTTP 500 spike", "--key", "alert-1")
	taskID := submitted["task"].(map[string]any)["id"].(string)
	code, ran := stopped.json("task", "run", taskID)
	if code != exitDecisionRequired || ran["task"].(map[string]any)["status"] != "WaitingApproval" {
		t.Fatalf("run: %d %v", code, ran)
	}
	firstExecution := ran["execution"].(map[string]any)["id"].(string)

	// A decision in a new process: approve, and the fake finishes the turn.
	fake, argsFile := recordingFakeAXLR(t, "completed")
	deciding := cli{t: t, state: state, axlr: fake}
	code, decided := deciding.json("task", "decide", taskID, "approve")
	if code != exitOK {
		t.Fatalf("decide: %d %v", code, decided)
	}
	task := decided["task"].(map[string]any)
	execution := decided["execution"].(map[string]any)
	if task["status"] != "Completed" || execution["outcome"] != "Completed" || execution["result"].(map[string]any)["answer"] != "pong" {
		t.Errorf("decide = %v", decided)
	}
	if ids, _ := json.Marshal(task["executionIds"]); string(ids) != `["`+firstExecution+`","`+execution["id"].(string)+`"]` {
		t.Errorf("executionIds = %s (first %s)", ids, firstExecution)
	}
	rawArgs, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(strings.Split(strings.TrimSpace(string(rawArgs)), "\n"), " ")
	if !strings.HasPrefix(args, "--session sess-1 --decide approve --json --approve none --call c1 --policy ") || !strings.Contains(args, " --root ") || strings.Contains(args, "--exec") {
		t.Errorf("axlr-tui args = %q", args)
	}
	entries, _ := os.ReadDir(filepath.Join(state, "executions"))
	if len(entries) != 2 {
		t.Errorf("%d executions, want 2", len(entries))
	}
	events, _ := os.ReadFile(filepath.Join(state, "events.jsonl"))
	if !strings.Contains(string(events), `"bot.task.decided"`) || !strings.Contains(string(events), `"callId":"c1"`) {
		t.Errorf("events = %s", events)
	}
	audit, _ := os.ReadFile(filepath.Join(state, "audit.jsonl"))
	if !strings.Contains(string(audit), `"task.decide"`) {
		t.Errorf("audit = %s", audit)
	}

	// Decided: a second decision is refused and the runtime not run.
	_ = os.Remove(argsFile)
	code, _, errText := deciding.run("task", "decide", taskID, "deny")
	if code != exitError || !strings.Contains(errText, "is Completed: cannot decide") {
		t.Errorf("second decision: %d %s", code, errText)
	}
	if _, err := os.Stat(argsFile); err == nil {
		t.Error("axlr-tui was run for a completed task")
	}
	// A Pending task has nothing to decide either.
	_, other := deciding.json("task", "submit", "server-incident-investigator", "--objective", "o", "--key", "alert-2")
	code, _, errText = deciding.run("task", "decide", other["task"].(map[string]any)["id"].(string), "approve")
	if code != exitError || !strings.Contains(errText, "is Pending: cannot decide") {
		t.Errorf("pending task: %d %s", code, errText)
	}
}

func TestTaskDecideExitCodesFollowTheOutcome(t *testing.T) {
	for scenario, want := range map[string]int{"decision": exitDecisionRequired, "failed": exitError} {
		state := t.TempDir()
		c := cli{t: t, state: state, axlr: fakeAXLR(t, "decision")}
		_, _ = c.json("bot", "register", "--file", definitionFile(t))
		_, _ = c.json("bot", "enable", "server-incident-investigator")
		_, submitted := c.json("task", "submit", "server-incident-investigator", "--objective", "o", "--key", "k")
		taskID := submitted["task"].(map[string]any)["id"].(string)
		_, _ = c.json("task", "run", taskID)
		c.axlr = fakeAXLR(t, scenario)
		code, decided := c.json("task", "decide", taskID, "deny")
		if code != want {
			t.Errorf("%s: exit %d, want %d (%v)", scenario, code, want, decided)
		}
	}
}

func TestUsageAndErrors(t *testing.T) {
	c := cli{t: t, state: t.TempDir(), axlr: fakeAXLR(t, "completed")}
	cases := []struct {
		args []string
		code int
		text string
	}{
		{nil, exitUsage, "usage:"},
		{[]string{"bot"}, exitUsage, "usage:"},
		{[]string{"bot", "explode"}, exitUsage, "unknown command"},
		{[]string{"bot", "register"}, exitUsage, "needs --file"},
		{[]string{"bot", "register", "--bogus"}, exitUsage, "flag provided but not defined"},
		{[]string{"bot", "register", "--file", "/no/such/file.json"}, exitError, "no such file"},
		{[]string{"bot", "enable"}, exitUsage, "exactly one"},
		{[]string{"bot", "enable", "Bad Id"}, exitError, "bot id"},
		{[]string{"bot", "enable", "ghost"}, exitError, "not found"},
		{[]string{"bot", "list", "extra"}, exitUsage, "no arguments"},
		{[]string{"bot", "get"}, exitUsage, "exactly one"},
		{[]string{"bot", "get", "Bad Id"}, exitError, "bot id"},
		{[]string{"bot", "get", "ghost"}, exitError, "not found"},
		{[]string{"task", "submit"}, exitUsage, "needs <bot-id>"},
		{[]string{"task", "submit", "--objective", "o"}, exitUsage, "needs <bot-id>"},
		{[]string{"task", "submit", "b", "--objective", "o"}, exitUsage, "needs --objective and --key"},
		{[]string{"task", "submit", "b", "--bogus"}, exitUsage, "flag provided but not defined"},
		{[]string{"task", "submit", "ghost", "--objective", "o", "--key", "k"}, exitError, "not found"},
		{[]string{"task", "run"}, exitUsage, "exactly one"},
		{[]string{"task", "run", "Bad Id"}, exitError, "task id"},
		{[]string{"task", "run", "ghost"}, exitError, "not found"},
		{[]string{"task", "decide"}, exitUsage, "needs <task-id> and approve|deny"},
		{[]string{"task", "decide", "t"}, exitUsage, "needs <task-id> and approve|deny"},
		{[]string{"task", "decide", "Bad Id", "approve"}, exitError, "task id"},
		{[]string{"task", "decide", "t", "maybe"}, exitUsage, "approve or deny"},
		{[]string{"task", "decide", "ghost", "approve"}, exitError, "not found"},
		{[]string{"task", "get"}, exitUsage, "exactly one"},
		{[]string{"task", "get", "Bad Id"}, exitError, "task id"},
		{[]string{"task", "get", "ghost"}, exitError, "not found"},
		{[]string{"task", "reconcile", "extra"}, exitUsage, "takes no arguments"},
		{[]string{"task", "reconcile", "--grace", "soon"}, exitUsage, "invalid value"},
		{[]string{"task", "reconcile", "--bogus"}, exitUsage, "flag provided but not defined"},
		{[]string{"execution", "get"}, exitUsage, "exactly one"},
		{[]string{"execution", "get", "Bad Id"}, exitError, "execution id"},
		{[]string{"execution", "get", "ghost"}, exitError, "not found"},
		{[]string{"task", "approve"}, exitUsage, "task approve needs exactly one"},
		{[]string{"task", "deny", "a", "b"}, exitUsage, "task deny needs exactly one"},
		{[]string{"task", "approve", "ghost"}, exitError, "not found"},
		{[]string{"task", "report"}, exitUsage, "exactly one"},
		{[]string{"task", "report", "--bogus", "t"}, exitUsage, "flag provided but not defined"},
		{[]string{"task", "report", "ghost"}, exitError, "not found"},
		{[]string{"alert"}, exitUsage, "needs --unit"},
		{[]string{"alert", "--unit", "x", "extra"}, exitUsage, "needs --unit"},
		{[]string{"alert", "--unit", "x", "--at", "yesterday"}, exitUsage, "RFC 3339"},
		{[]string{"alert", "--bogus"}, exitUsage, "flag provided but not defined"},
		{[]string{"serve", "extra"}, exitUsage, "takes no arguments"},
		{[]string{"serve", "--bogus"}, exitUsage, "flag provided but not defined"},
		{[]string{"--socket", "", "serve"}, exitUsage, "needs a socket"},
		{[]string{"install", "--bogus"}, exitError, ""},
		{[]string{"uninstall", "--bogus"}, exitError, ""},
	}
	for _, tc := range cases {
		code, _, errText := c.run(tc.args...)
		if code != tc.code || !strings.Contains(errText, tc.text) {
			t.Errorf("%v: exit %d stderr %q; want %d containing %q", tc.args, code, errText, tc.code, tc.text)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--help"}, &stdout, &stderr); code != exitOK || !strings.Contains(stderr.String(), "usage:") {
		t.Errorf("--help: %d %s", code, stderr.String())
	}
	if code := run([]string{"--bogus"}, &stdout, &stderr); code != exitUsage {
		t.Errorf("--bogus: %d", code)
	}
}

func TestBadDefinitionsAndState(t *testing.T) {
	c := cli{t: t, state: t.TempDir(), axlr: fakeAXLR(t, "completed")}
	dir := t.TempDir()
	unknown := filepath.Join(dir, "unknown.json")
	_ = os.WriteFile(unknown, []byte(`{"tenant":"t","name":"x","surprise":1}`), 0o644)
	if code, _, errText := c.run("bot", "register", "--file", unknown); code != exitError || !strings.Contains(errText, "unknown field") {
		t.Errorf("unknown field: %d %s", code, errText)
	}
	invalid := filepath.Join(dir, "invalid.json")
	_ = os.WriteFile(invalid, []byte(`{"tenant":"","name":"x"}`), 0o644)
	if code, _, errText := c.run("bot", "register", "--file", invalid); code != exitError || !strings.Contains(errText, "tenant id") {
		t.Errorf("invalid definition: %d %s", code, errText)
	}
	_, _ = c.json("bot", "register", "--file", definitionFile(t))
	if code, _, errText := c.run("bot", "register", "--file", definitionFile(t)); code != exitError || !strings.Contains(errText, "already exists") {
		t.Errorf("duplicate: %d %s", code, errText)
	}
	if code, _, errText := c.run("task", "submit", "server-incident-investigator", "--objective", "o", "--key", "k"); code != exitError || !strings.Contains(errText, "is Registered") {
		t.Errorf("submit to a registered bot: %d %s", code, errText)
	}

	var stdout, stderr bytes.Buffer
	blocker := filepath.Join(dir, "blocker")
	_ = os.WriteFile(blocker, []byte("x"), 0o644)
	if code := run([]string{"--state", filepath.Join(blocker, "state"), "bot", "list"}, &stdout, &stderr); code != exitError {
		t.Errorf("state under a file: %d", code)
	}
	if code := run([]string{"--state", "", "bot", "list"}, &stdout, &stderr); code != exitError {
		t.Errorf("empty state: %d", code)
	}
}

func TestDefaults(t *testing.T) {
	t.Setenv("UNDERPASS_STATE", "/tmp/custom-state")
	if defaultStateDir() != "/tmp/custom-state" {
		t.Error("UNDERPASS_STATE ignored")
	}
	t.Setenv("UNDERPASS_STATE", "")
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg")
	if defaultStateDir() != "/tmp/xdg/underpass-bots" {
		t.Error("XDG_STATE_HOME ignored")
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/someone")
	if defaultStateDir() != "/home/someone/.local/state/underpass-bots" {
		t.Errorf("default = %s", defaultStateDir())
	}
	t.Setenv("UNDERPASS_AXLR_BIN", "/opt/axlr-tui")
	if defaultAXLRBinary() != "/opt/axlr-tui" {
		t.Error("UNDERPASS_AXLR_BIN ignored")
	}
	t.Setenv("UNDERPASS_AXLR_BIN", "")
	if defaultAXLRBinary() != "axlr-tui" {
		t.Error("default binary wrong")
	}
	t.Setenv("USER", "ana")
	if defaultActor() != "cli:ana" {
		t.Error("actor wrong")
	}
	t.Setenv("USER", "")
	if defaultActor() != "cli:unknown" {
		t.Error("fallback actor wrong")
	}
	t.Setenv("UNDERPASS_SOCKET", "/run/cp.sock")
	if defaultSocket() != "/run/cp.sock" {
		t.Error("UNDERPASS_SOCKET ignored")
	}
	t.Setenv("UNDERPASS_SOCKET", "")
	if defaultSocket() != "" {
		t.Error("default socket should be empty until the state is known")
	}
	if envOr("UNDERPASS_NOPE", "x") != "x" || hostname() == "" {
		t.Error("envOr or hostname wrong")
	}
	var out bytes.Buffer
	printJSON(&out, make(chan int))
	if strings.TrimSpace(out.String()) != "{}" {
		t.Errorf("printJSON fallback = %q", out.String())
	}
}

// crashed stands for a process that died mid-run: it writes the task and
// its execution as Running through the real repositories, as RunTask does
// before calling the runtime, and nothing ends them.
func crashed(t *testing.T, state, taskID string, startedAt time.Time) {
	t.Helper()
	ctx := context.Background()
	task, err := domain.NewTask(domain.TaskID(taskID), "server-incident-investigator", "Investigate", domain.IdempotencyKey("key-"+taskID), "trace-abc", startedAt)
	if err != nil {
		t.Fatal(err)
	}
	executionID := domain.ExecutionID("exec-" + taskID)
	if err := task.Start(executionID, startedAt); err != nil {
		t.Fatal(err)
	}
	execution, err := domain.NewExecution(executionID, task.ID(), task.BotID(), startedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := filestore.NewExecutionRepository(state).Save(ctx, execution); err != nil {
		t.Fatal(err)
	}
	if err := filestore.NewTaskRepository(state).Save(ctx, task); err != nil {
		t.Fatal(err)
	}
}

func TestTaskReconcileFailsWhatACrashLeftRunning(t *testing.T) {
	state := t.TempDir()
	c := cli{t: t, state: state, axlr: fakeAXLR(t, "completed")}
	_, _ = c.json("bot", "register", "--file", definitionFile(t))
	_, _ = c.json("bot", "enable", "server-incident-investigator")
	// The example bot's timeout is 900s: the default grace is 16 minutes.
	crashed(t, state, "task-old", time.Now().Add(-time.Hour))
	crashed(t, state, "task-young", time.Now().Add(-time.Minute))
	tracePath := filepath.Join(state, "traces", "exec-task-old.jsonl")
	_ = os.MkdirAll(filepath.Dir(tracePath), 0o755)
	_ = os.WriteFile(tracePath, []byte(`{"type":"session","session":"sess-1"}`+"\n"), 0o644)

	code, out := c.json("task", "reconcile")
	if code != exitOK {
		t.Fatalf("reconcile: %d %v", code, out)
	}
	if failed, _ := json.Marshal(out["failed"]); string(failed) != `["task-old"]` {
		t.Errorf("failed = %s", failed)
	}
	if skipped, _ := json.Marshal(out["skipped"]); string(skipped) != `["task-young"]` {
		t.Errorf("skipped = %s", skipped)
	}
	code, task := c.json("task", "get", "task-old")
	reason, _ := task["failureReason"].(string)
	// The age is read from the wall clock: a slow disk may make it 1h0m1s.
	if code != exitOK || task["status"] != "Failed" || !strings.HasPrefix(reason, "reconciled: the control plane found it Running after 1h0m") || !strings.HasSuffix(reason, "; the process that ran it is gone") {
		t.Errorf("old task: %d %v", code, task)
	}
	code, execution := c.json("execution", "get", "exec-task-old")
	if code != exitOK || execution["outcome"] != "Failed" || execution["failureReason"] != reason || execution["tracePath"] != tracePath || execution["endedAt"] == nil {
		t.Errorf("old execution: %d %v", code, execution)
	}
	code, task = c.json("task", "get", "task-young")
	if code != exitOK || task["status"] != "Running" {
		t.Errorf("young task: %d %v", code, task)
	}
	code, execution = c.json("execution", "get", "exec-task-young")
	if code != exitOK || execution["outcome"] != "Running" {
		t.Errorf("young execution: %d %v", code, execution)
	}
	events, _ := os.ReadFile(filepath.Join(state, "events.jsonl"))
	if !strings.Contains(string(events), `"reason":"reconciled"`) || strings.Count(string(events), "bot.task.failed") != 1 {
		t.Errorf("events: %s", events)
	}
	audit, _ := os.ReadFile(filepath.Join(state, "audit.jsonl"))
	if !strings.Contains(string(audit), `"action":"task.reconcile"`) {
		t.Errorf("audit: %s", audit)
	}

	// Nothing left to do: the failed task is no longer Running, the young
	// one is still within its grace. A shorter grace fails the young one too.
	code, out = c.json("task", "reconcile")
	if failed, _ := json.Marshal(out["failed"]); code != exitOK || string(failed) != `[]` {
		t.Errorf("second reconcile: %d %v", code, out)
	}
	code, out = c.json("task", "reconcile", "--grace", "30s")
	if failed, _ := json.Marshal(out["failed"]); code != exitOK || string(failed) != `["task-young"]` {
		t.Errorf("short grace: %d %v", code, out)
	}
	code, execution = c.json("execution", "get", "exec-task-young")
	if code != exitOK || execution["outcome"] != "Failed" || execution["tracePath"] != nil {
		t.Errorf("young execution after a short grace: %d %v", code, execution)
	}

	// A Running task whose bot is gone is an error, not silence.
	crashed(t, state, "task-orphan", time.Now().Add(-time.Hour))
	_ = os.Remove(filepath.Join(state, "bots", "server-incident-investigator.json"))
	if code, _, errText := c.run("task", "reconcile"); code != exitError || !strings.Contains(errText, "reconcile task task-orphan") {
		t.Errorf("orphan: %d %s", code, errText)
	}
}
