package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/pki"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
)

const cliSecret = "cli-secret-0123456789"

// triggeredBotFile writes a definition with a webhook and a timer trigger.
func triggeredBotFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bot.json")
	definition := `{
  "id": "bot", "tenant": "default", "name": "bot", "owner": "operator",
  "mission": "Do what each task asks, within the policy.",
  "level": "observe", "allowedTools": ["local_read"],
  "triggers": [
    {"type": "manual"},
    {"type": "webhook", "name": "deploy", "objective": "Handle event {{.type}} for {{.service}}", "secret": "` + cliSecret + `"},
    {"type": "timer", "name": "tick", "objective": "say the time (fired at {{.firedAt}})", "every": "10m"}
  ],
  "model": {"model": "anthropic/claude-haiku-5.5"},
  "budgets": {"taskTimeoutSeconds": 60}
}`
	if err := os.WriteFile(path, []byte(definition), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTriggerAndTaskCommandsWithoutADaemon(t *testing.T) {
	c := cli{t: t, state: t.TempDir(), axlr: fakeAXLR(t, "completed"), socket: ""}
	if code, _, errText := c.run("bot", "register", "--file", triggeredBotFile(t)); code != exitOK {
		t.Fatalf("register: %d %s", code, errText)
	}
	if code, bot := c.json("bot", "enable", "bot"); code != exitOK || bot["triggers"].([]any)[1].(map[string]any)["secret"] != "<redacted>" {
		t.Errorf("enable: %d %v", code, bot)
	}
	patch := filepath.Join(t.TempDir(), "patch.json")
	_ = os.WriteFile(patch, []byte(`{"mission": "A patched mission"}`), 0o600)
	if code, bot := c.json("bot", "update", "bot", "--file", patch); code != exitOK || bot["mission"] != "A patched mission" {
		t.Errorf("update: %d %v", code, bot)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(bad, []byte(`{"missionn": "typo"}`), 0o600)
	if code, _, errText := c.run("bot", "update", "bot", "--file", bad); code != exitError || !strings.Contains(errText, "unknown field") {
		t.Errorf("unknown field: %d %s", code, errText)
	}
	if code, _, _ := c.run("bot", "update", "bot", "--file", filepath.Join(t.TempDir(), "missing.json")); code != exitError {
		t.Error("missing patch file accepted")
	}
	code, out, _ := c.run("bot", "events", "bot", "--limit", "2")
	var events []map[string]any
	if err := json.Unmarshal([]byte(out), &events); code != exitOK || err != nil || len(events) != 2 || events[1]["type"] != "bot.updated" {
		t.Errorf("events: %d %s", code, out)
	}
	code, fired := c.json("trigger", "fire", "bot", "deploy", "--body", `{"type": "deploy", "service": "web"}`, "--delivery", "d-1")
	if code != exitOK || fired["created"] != true || fired["task"].(map[string]any)["objective"] != "Handle event deploy for web" {
		t.Fatalf("fire: %d %v", code, fired)
	}
	taskID := fired["task"].(map[string]any)["id"].(string)
	if code, again := c.json("trigger", "fire", "bot", "deploy", "--body", `{"type": "deploy", "service": "web"}`, "--delivery", "d-1"); code != exitOK || again["created"] != false || again["task"].(map[string]any)["id"] != taskID {
		t.Errorf("redelivery: %d %v", code, again)
	}
	event := filepath.Join(t.TempDir(), "event.json")
	_ = os.WriteFile(event, []byte(`{"type": "rollback", "service": "api"}`), 0o600)
	if code, fromFile := c.json("trigger", "fire", "bot", "deploy", "--file", event); code != exitOK || fromFile["created"] != true {
		t.Errorf("fire from file: %d %v", code, fromFile)
	}
	if code, _, errText := c.run("trigger", "fire", "bot", "deploy", "--body", `[1]`); code != exitError || !strings.Contains(errText, "JSON object") {
		t.Errorf("array body: %d %s", code, errText)
	}
	code, out, _ = c.run("task", "executions", taskID)
	if code != exitOK || strings.TrimSpace(out) != "[]" {
		t.Errorf("executions before a run: %d %s", code, out)
	}
	if code, cancelled := c.json("task", "cancel", taskID); code != exitOK || cancelled["changed"] != true || cancelled["task"].(map[string]any)["status"] != "Cancelled" {
		t.Errorf("cancel: %d %v", code, cancelled)
	}
	if code, cancelled := c.json("task", "cancel", taskID); code != exitOK || cancelled["changed"] != false {
		t.Errorf("cancel again: %d %v", code, cancelled)
	}
	// A completed task: run it, list its execution with its correlation, refuse the cancel.
	code, submitted := c.json("task", "submit", "bot", "--objective", "ping", "--key", "k-1")
	if code != exitOK {
		t.Fatal("submit failed")
	}
	done := submitted["task"].(map[string]any)["id"].(string)
	if code, _, _ := c.run("task", "run", done); code != exitOK {
		t.Fatal("run failed")
	}
	code, out, _ = c.run("task", "executions", done)
	var executions []map[string]any
	if err := json.Unmarshal([]byte(out), &executions); code != exitOK || err != nil || len(executions) != 1 || executions[0]["correlation"].(map[string]any)["traceId"] != submitted["task"].(map[string]any)["correlationId"] || executions[0]["correlation"].(map[string]any)["carried"] != false {
		t.Errorf("executions: %d %s", code, out)
	}
	if code, _, errText := c.run("task", "cancel", done); code != exitError || !strings.Contains(errText, "Completed") {
		t.Errorf("cancel a completed task: %d %s", code, errText)
	}
	for _, args := range [][]string{
		{"bot", "update"}, {"bot", "update", "bot"}, {"bot", "update", "bot", "--bogus"}, {"bot", "update", "bot", "--file", patch, "extra"},
		{"bot", "events"}, {"bot", "events", "bot", "extra"}, {"bot", "events", "bot", "--limit", "x"},
		{"task", "cancel"}, {"task", "executions"}, {"task", "executions", "a", "b"},
		{"trigger", "fire"}, {"trigger", "fire", "bot"}, {"trigger", "fire", "bot", "deploy"}, {"trigger", "fire", "bot", "deploy", "--body", "{}", "--file", event},
		{"trigger", "fire", "bot", "deploy", "--bogus"}, {"trigger", "fire", "bot", "deploy", "--body", "{}", "extra"},
	} {
		if code, _, _ := c.run(args...); code != exitUsage {
			t.Errorf("%v: exit %d, want usage", args, code)
		}
	}
	if code, _, _ := c.run("trigger", "fire", "bot", "deploy", "--file", filepath.Join(t.TempDir(), "none.json")); code != exitError {
		t.Error("missing event file accepted")
	}
}

func TestServeWithTheOperatorAddressFiresWebhooksAndTimers(t *testing.T) {
	state := t.TempDir()
	socket := filepath.Join(t.TempDir(), "cp.sock")
	c := cli{t: t, state: state, axlr: fakeAXLR(t, "completed"), socket: ""}
	var out, errText bytes.Buffer
	if code := run([]string{"--state", state, "--socket", socket, "serve", "--api-listen", "127.0.0.1:0"}, &out, &errText); code != exitError || !strings.Contains(errText.String(), "ca init") {
		t.Fatalf("serve --api-listen without a CA: %d %s", code, errText.String())
	}
	if code, _ := c.json("ca", "init"); code != exitOK {
		t.Fatal("ca init failed")
	}
	if code, _, errText := c.run("bot", "register", "--file", triggeredBotFile(t)); code != exitOK {
		t.Fatalf("register: %d %s", code, errText)
	}
	if code, _, _ := c.run("bot", "enable", "bot"); code != exitOK {
		t.Fatal("enable failed")
	}
	port := freePort(t)
	var serveOut, serveErr syncBuffer
	done := make(chan int, 1)
	go func() {
		done <- run([]string{"--state", state, "--axlr-bin", c.axlr, "--socket", socket, "serve", "--api-listen", "127.0.0.1:" + port, "--tick", "20ms"}, &serveOut, &serveErr)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !unixapi.Reachable(t.Context(), socket) {
		if time.Now().After(deadline) {
			t.Fatalf("daemon not reachable: %s", serveErr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	tokenPath := filepath.Join(state, "api.token")
	raw, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("token file: %v", err)
	}
	if info, _ := os.Stat(tokenPath); info.Mode().Perm() != 0o600 || len(strings.TrimSpace(string(raw))) != 64 {
		t.Errorf("token file mode %v, %d bytes", info.Mode().Perm(), len(raw))
	}
	token := strings.TrimSpace(string(raw))
	if strings.Contains(serveErr.String(), token) {
		t.Error("the token was logged")
	}
	authority, _ := pki.Load(filepath.Join(state, pki.CADirName))
	config, _ := pki.EnrollTLSConfig(authority.CertificatePEM())
	url := "https://127.0.0.1:" + port
	api := unixapi.NewTLSClient(url, config.Clone()).WithToken(token)
	deadline = time.Now().Add(5 * time.Second)
	for {
		if _, err = api.ListBots(context.Background()); err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("operator address: %v\n%s", err, serveErr.String())
	}
	var unauthorized unixapi.UnauthorizedError
	if _, err := unixapi.NewTLSClient(url, config.Clone()).ListBots(context.Background()); !errors.As(err, &unauthorized) {
		t.Errorf("without the token: %v", err)
	}
	// A webhook from outside, with the trigger's secret, as curl would send it.
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: config.Clone()}}
	post := func(delivery string) map[string]any {
		t.Helper()
		request, _ := http.NewRequest(http.MethodPost, url+"/v1/triggers/bot/deploy", strings.NewReader(`{"type": "deploy", "service": "web"}`))
		request.Header.Set(unixapi.WebhookSecretHeader, cliSecret)
		request.Header.Set(unixapi.IdempotencyKeyHeader, delivery)
		response, err := httpClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var decoded map[string]any
		_ = json.NewDecoder(response.Body).Decode(&decoded)
		if response.StatusCode >= 400 {
			t.Fatalf("webhook: %d %v", response.StatusCode, decoded)
		}
		return decoded
	}
	first := post("gh-1")
	if first["created"] != true {
		t.Errorf("first delivery: %v", first)
	}
	if again := post("gh-1"); again["created"] != false || again["task"].(map[string]any)["id"] != first["task"].(map[string]any)["id"] {
		t.Errorf("redelivery: %v", again)
	}
	c.socket = socket
	webhookTask := first["task"].(map[string]any)["id"].(string)
	waitForStatus(t, c, webhookTask, "Completed")
	// The timer fired at once when it was armed; its task completes too.
	var timerTask string
	deadline = time.Now().Add(5 * time.Second)
	for timerTask == "" {
		_, out, _ := c.run("bot", "events", "bot")
		var events []map[string]any
		_ = json.Unmarshal([]byte(out), &events)
		for _, e := range events {
			if e["type"] == "bot.trigger.fired" && e["payload"].(map[string]any)["type"] == "timer" {
				timerTask = e["payload"].(map[string]any)["taskId"].(string)
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the timer did not fire: %s", serveErr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	timer := waitForStatus(t, c, timerTask, "Completed")
	if !strings.HasPrefix(timer["objective"].(string), "say the time (fired at 20") || timer["trigger"].(map[string]any)["name"] != "tick" {
		t.Errorf("timer task = %v", timer)
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	select {
	case code := <-done:
		if code != exitOK {
			t.Errorf("serve exit %d: %s", code, serveErr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop")
	}
	for _, want := range []string{"operator token written to " + tokenPath, "(operators, bearer token)", "daemon: timer bot/tick fired"} {
		if !strings.Contains(serveErr.String(), want) {
			t.Errorf("serve log lacks %q:\n%s", want, serveErr.String())
		}
	}
	// A second start reads the token back.
	var again bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		done <- serve(ctx, mustApp(t, state, c.axlr), state, socket, []string{"--api-listen", "127.0.0.1:0"}, &out, &again)
	}()
	for !unixapi.Reachable(context.Background(), socket) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if !strings.Contains(again.String(), "operator token read from "+tokenPath) {
		t.Errorf("second start log = %s", again.String())
	}
}

func mustApp(t *testing.T, state, axlr string) application.Dependencies {
	t.Helper()
	deps, err := newApp(state, axlr, "test:cli")
	if err != nil {
		t.Fatal(err)
	}
	return deps
}

func waitForStatus(t *testing.T, c cli, id, status string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, task := c.json("task", "get", id)
		if task["status"] == status {
			return task
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %s is %v, want %s", id, task["status"], status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAPITokenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "api.token")
	token, created, err := loadOrCreateAPIToken(path)
	if err != nil || !created || len(token) != 64 {
		t.Fatalf("create: %v %v %q", err, created, token)
	}
	same, created, err := loadOrCreateAPIToken(path)
	if err != nil || created || same != token {
		t.Errorf("read back: %v %v", err, created)
	}
	empty := filepath.Join(dir, "empty")
	_ = os.WriteFile(empty, []byte("\n"), 0o600)
	if _, _, err := loadOrCreateAPIToken(empty); err == nil {
		t.Error("an empty file accepted")
	}
	if _, _, err := loadOrCreateAPIToken(dir); err == nil {
		t.Error("a directory accepted")
	}
	blocked := filepath.Join(empty, "api.token")
	if _, _, err := loadOrCreateAPIToken(blocked); err == nil {
		t.Error("a path under a file accepted")
	}
}

func TestInstallRendersTheOperatorAddress(t *testing.T) {
	dest := t.TempDir()
	t.Setenv("HOME", "/home/ana")
	nodeBinary := filepath.Join(t.TempDir(), "axlr-node")
	_ = os.WriteFile(nodeBinary, []byte("#!/bin/sh\n"), 0o755)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"install", "--destdir", dest, "--no-systemctl", "--node-binary", nodeBinary, "--listen", "127.0.0.1:7878", "--api-listen", "127.0.0.1:7879"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("install: %d %s", code, stderr.String())
	}
	var report map[string]any
	_ = json.Unmarshal(stdout.Bytes(), &report)
	if report["apiListen"] != "127.0.0.1:7879" || !strings.Contains(stdout.String(), "api.token") {
		t.Errorf("report = %v", report)
	}
	service, _ := os.ReadFile(filepath.Join(dest, "home/ana/.config/systemd/user/axlr-bots.service"))
	if !strings.Contains(string(service), " --listen 127.0.0.1:7878 --api-listen 127.0.0.1:7879\n") {
		t.Errorf("service:\n%s", service)
	}
}
