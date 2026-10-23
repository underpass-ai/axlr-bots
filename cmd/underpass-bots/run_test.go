package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeAXLR writes a shell stand-in for axlr-tui that prints the lines of
// the scenario and exits with its code.
func fakeAXLR(t *testing.T, scenario string) string {
	t.Helper()
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
	path := filepath.Join(t.TempDir(), "fake-axlr-tui")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func definitionFile(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "examples", "server-incident-investigator.json")
}

type cli struct {
	t     *testing.T
	state string
	axlr  string
}

func (c cli) run(args ...string) (int, string, string) {
	c.t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(append([]string{"--state", c.state, "--axlr-bin", c.axlr, "--actor", "test:cli"}, args...), &stdout, &stderr)
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
		{[]string{"task", "get"}, exitUsage, "exactly one"},
		{[]string{"task", "get", "Bad Id"}, exitError, "task id"},
		{[]string{"task", "get", "ghost"}, exitError, "not found"},
		{[]string{"execution", "get"}, exitUsage, "exactly one"},
		{[]string{"execution", "get", "Bad Id"}, exitError, "execution id"},
		{[]string{"execution", "get", "ghost"}, exitError, "not found"},
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
	var out bytes.Buffer
	printJSON(&out, make(chan int))
	if strings.TrimSpace(out.String()) != "{}" {
		t.Errorf("printJSON fallback = %q", out.String())
	}
}
