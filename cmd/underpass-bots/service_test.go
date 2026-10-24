package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/underpass-ai/underpass-bots/adapters/unixapi"
)

func sreDefinitionFile(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "examples", "sre-host-bot.json")
}

// TestServeRunsWhatTheCommandsSendOverTheSocket starts the daemon with
// the serve command, drives it with the other commands over its socket
// and stops it with SIGINT, as systemd would with SIGTERM.
func TestServeRunsWhatTheCommandsSendOverTheSocket(t *testing.T) {
	state := t.TempDir()
	socket := filepath.Join(t.TempDir(), "cp.sock")
	fake := fakeAXLR(t, "completed")
	var serveOut, serveErr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- run([]string{"--state", state, "--axlr-bin", fake, "--socket", socket, "serve", "--tick", "20ms", "--reconcile-every", "50ms"}, &serveOut, &serveErr)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !unixapi.Reachable(t.Context(), socket) {
		if time.Now().After(deadline) {
			t.Fatalf("daemon not reachable: %s", serveErr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	c := cli{t: t, state: state, axlr: "/nonexistent/axlr-tui", socket: socket}
	if code, bot := c.json("bot", "register", "--file", sreDefinitionFile(t)); code != exitOK || bot["level"] != "observe" {
		t.Fatalf("register: %d %v", code, bot)
	}
	if code, _ := c.json("bot", "enable", "sre-host-bot"); code != exitOK {
		t.Fatal("enable")
	}
	code, alerted := c.json("alert", "--unit", "demo-broken", "--user", "--at", "2026-10-24T10:37:12Z")
	if code != exitOK || alerted["created"] != true {
		t.Fatalf("alert: %d %v", code, alerted)
	}
	task := alerted["task"].(map[string]any)
	taskID := task["id"].(string)
	if task["idempotencyKey"] != "alert:demo-broken.service:2026-10-24T10:30:00Z" || !strings.Contains(task["objective"].(string), "systemctl --user status demo-broken.service") {
		t.Errorf("task = %v", task)
	}
	// The daemon runs it: the command's axlr-bin is unusable, so a run
	// in this process would fail.
	deadline = time.Now().Add(5 * time.Second)
	for {
		_, got := c.json("task", "get", taskID)
		if got["status"] == "Completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task not run by the daemon: %v\n%s", got, serveErr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	code, out, _ := c.run("task", "report", taskID)
	if code != exitOK || !strings.HasPrefix(out, "pong") {
		t.Errorf("report: %d %q", code, out)
	}
	code, out, _ = c.run("task", "report", "--path", taskID)
	if code != exitOK || !strings.HasPrefix(strings.TrimSpace(out), filepath.Join(state, "artifacts", taskID)) {
		t.Errorf("report path: %d %q", code, out)
	}
	if _, err := os.ReadFile(strings.TrimSpace(out)); err != nil {
		t.Error(err)
	}
	code, again := c.json("alert", "--unit", "demo-broken.service", "--user", "--at", "2026-10-24T10:39:00Z")
	if code != exitOK || again["created"] != false || again["task"].(map[string]any)["id"] != taskID {
		t.Errorf("repeated alert: %d %v", code, again)
	}
	code, submitted := c.json("task", "submit", "sre-host-bot", "--objective", "Look around", "--key", "manual-1")
	if code != exitOK {
		t.Fatal("submit")
	}
	manual := submitted["task"].(map[string]any)["id"].(string)
	// A run asked over the socket is carried by the daemon too, and a
	// second one is refused by it.
	code, ran := c.json("task", "run", manual)
	if code != exitOK && code != exitError {
		t.Fatalf("run over the socket: %d %v", code, ran)
	}
	if code, _, errText := c.run("task", "run", manual); code != exitError || !strings.Contains(errText, "cannot start") {
		t.Errorf("second run: %d %s", code, errText)
	}
	if code, _, errText := c.run("task", "approve", manual); code != exitError || !strings.Contains(errText, "cannot decide") {
		t.Errorf("approve a completed task: %d %s", code, errText)
	}
	if code, execution := c.json("execution", "get", ran["execution"].(map[string]any)["id"].(string)); code != exitOK || execution["policy"].(map[string]any)["enforced"] != true {
		t.Errorf("execution over the socket: %d %v", code, execution)
	}
	if code, bots, _ := c.run("bot", "list"); code != exitOK || !strings.Contains(bots, "sre-host-bot") {
		t.Errorf("list over the socket: %d", code)
	}
	if code, _, errText := c.run("bot", "get", "ghost"); code != exitError || !strings.Contains(errText, "not found") {
		t.Errorf("missing bot over the socket: %d %s", code, errText)
	}

	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != exitOK {
			t.Errorf("serve exited %d: %s", code, serveErr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop on SIGINT")
	}
	if _, err := os.Stat(socket); err == nil {
		t.Error("socket left behind")
	}
	if !strings.Contains(serveErr.String(), "task "+taskID+" Completed") || !strings.Contains(serveErr.String(), "daemon: stopped") {
		t.Errorf("serve log = %s", serveErr.String())
	}
	// With the daemon gone the commands fall back to the state directory.
	if code, got := c.json("task", "get", taskID); code != exitOK || got["status"] != "Completed" {
		t.Errorf("after stop: %d %v", code, got)
	}
}

func TestAlertSpoolsWhenNoServiceAnswers(t *testing.T) {
	state := t.TempDir()
	c := cli{t: t, state: state, axlr: fakeAXLR(t, "completed"), socket: filepath.Join(t.TempDir(), "nobody.sock")}
	_, _ = c.json("bot", "register", "--file", sreDefinitionFile(t))
	_, _ = c.json("bot", "enable", "sre-host-bot")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--state", state, "--axlr-bin", c.axlr, "--socket", c.socket, "alert", "--unit", "demo-broken", "--host", "box", "--window", "1h", "--correlation", "sd-1"}, &stdout, &stderr)
	if code != exitOK || !strings.Contains(stderr.String(), "no service answers") {
		t.Fatalf("alert: %d %s", code, stderr.String())
	}
	var alerted map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &alerted); err != nil {
		t.Fatal(err)
	}
	task := alerted["task"].(map[string]any)
	if task["status"] != "Pending" || task["correlationId"] != "sd-1" || !strings.Contains(task["objective"].(string), "host box") || strings.Contains(task["objective"].(string), "--user") {
		t.Errorf("task = %v", task)
	}
	if !strings.HasPrefix(task["idempotencyKey"].(string), "alert:demo-broken.service:") || !strings.HasSuffix(task["idempotencyKey"].(string), ":00:00Z") {
		t.Errorf("key = %v", task["idempotencyKey"])
	}
	t.Setenv("UNDERPASS_BOT", "ghost")
	t.Setenv("UNDERPASS_SYSTEMD_USER", "1")
	if code, _, errText := c.run("alert", "--unit", "x"); code != exitError || !strings.Contains(errText, "not found") {
		t.Errorf("unknown bot from the environment: %d %s", code, errText)
	}
	// The default socket under the state directory is not there either:
	// the commands work on the files without a notice.
	plain := cli{t: t, state: state, axlr: c.axlr}
	if code, _, errText := plain.run("task", "get", task["id"].(string)); code != exitOK || errText != "" {
		t.Errorf("default socket: %d %s", code, errText)
	}
}

func TestTaskReportAndApprovalWithoutADaemon(t *testing.T) {
	state := t.TempDir()
	c := cli{t: t, state: state, axlr: fakeAXLR(t, "decision")}
	_, _ = c.json("bot", "register", "--file", sreDefinitionFile(t))
	_, _ = c.json("bot", "enable", "sre-host-bot")
	_, submitted := c.json("task", "submit", "sre-host-bot", "--objective", "o", "--key", "k")
	taskID := submitted["task"].(map[string]any)["id"].(string)
	if code, _ := c.json("task", "run", taskID); code != exitDecisionRequired {
		t.Fatalf("run: %d", code)
	}
	if code, _, errText := c.run("task", "report", taskID); code != exitError || !strings.Contains(errText, "report "+taskID+" not found") {
		t.Errorf("report before completion: %d %s", code, errText)
	}
	c.axlr = fakeAXLR(t, "completed")
	code, decided := c.json("task", "deny", taskID)
	if code != exitOK || decided["task"].(map[string]any)["status"] != "Completed" {
		t.Fatalf("deny: %d %v", code, decided)
	}
	if code, out, _ := c.run("task", "report", taskID); code != exitOK || out != "pong\n" {
		t.Errorf("report: %d %q", code, out)
	}
}
