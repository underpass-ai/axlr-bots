package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
)

func sreDefinitionFile(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "examples", "bot.json")
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
	if code, bot := c.json("bot", "register", "--file", sreDefinitionFile(t)); code != exitOK || bot["level"] != "observe" || bot["triggers"].([]any)[0].(map[string]any)["type"] != "manual" {
		t.Fatalf("register: %d %v", code, bot)
	}
	if code, _ := c.json("bot", "enable", "bot"); code != exitOK {
		t.Fatal("enable")
	}
	code, submitted1 := c.json("task", "submit", "bot", "--objective", "List the files in your workspace and say how many there are", "--key", "demo-1")
	if code != exitOK || submitted1["created"] != true {
		t.Fatalf("submit: %d %v", code, submitted1)
	}
	task := submitted1["task"].(map[string]any)
	taskID := task["id"].(string)
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
	code, again := c.json("task", "submit", "bot", "--objective", "List the files in your workspace and say how many there are", "--key", "demo-1")
	if code != exitOK || again["created"] != false || again["task"].(map[string]any)["id"] != taskID {
		t.Errorf("repeated submission: %d %v", code, again)
	}
	if code, nodes, _ := c.run("node", "list"); code != exitOK || strings.TrimSpace(nodes) != "[]" {
		t.Errorf("node list over the socket: %d %q", code, nodes)
	}
	if code, _, errText := c.run("node", "get", "ghost"); code != exitError || !strings.Contains(errText, "not found") {
		t.Errorf("node get over the socket: %d %s", code, errText)
	}
	code, submitted := c.json("task", "submit", "bot", "--objective", "Look around", "--key", "manual-1")
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
	// The daemon's own tick may have started the task before the run
	// asked over the socket; either way the execution is the task's.
	_, got := c.json("task", "get", manual)
	executionID := got["executionIds"].([]any)[0].(string)
	if ran["execution"] != nil && ran["execution"].(map[string]any)["id"] != executionID {
		t.Errorf("run over the socket started a second execution: %v", ran)
	}
	if code, execution := c.json("execution", "get", executionID); code != exitOK || execution["policy"].(map[string]any)["enforced"] != true {
		t.Errorf("execution over the socket: %d %v", code, execution)
	}
	if code, bots, _ := c.run("bot", "list"); code != exitOK || !strings.Contains(bots, "bot") {
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

func TestSubmitSpoolsWhenNoServiceAnswers(t *testing.T) {
	state := t.TempDir()
	c := cli{t: t, state: state, axlr: fakeAXLR(t, "completed"), socket: filepath.Join(t.TempDir(), "nobody.sock")}
	_, _ = c.json("bot", "register", "--file", sreDefinitionFile(t))
	_, _ = c.json("bot", "enable", "bot")
	code, submitted := c.json("task", "submit", "bot", "--objective", "List the files in your workspace", "--key", "k-1")
	if code != exitOK || submitted["task"].(map[string]any)["status"] != "Pending" {
		t.Fatalf("submit without a daemon: %d %v", code, submitted)
	}
	// The default socket under the state directory is not there either:
	// the commands work on the files without a notice.
	plain := cli{t: t, state: state, axlr: c.axlr}
	if code, _, errText := plain.run("task", "get", submitted["task"].(map[string]any)["id"].(string)); code != exitOK || errText != "" {
		t.Errorf("default socket: %d %s", code, errText)
	}
	if code, out, _ := plain.run("node", "list"); code != exitOK || strings.TrimSpace(out) != "[]" {
		t.Errorf("node list: %d %q", code, out)
	}
}

func TestTaskReportAndApprovalWithoutADaemon(t *testing.T) {
	state := t.TempDir()
	c := cli{t: t, state: state, axlr: fakeAXLR(t, "decision")}
	_, _ = c.json("bot", "register", "--file", sreDefinitionFile(t))
	_, _ = c.json("bot", "enable", "bot")
	_, submitted := c.json("task", "submit", "bot", "--objective", "o", "--key", "k")
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
