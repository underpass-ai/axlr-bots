package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/system"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/compose"
	"github.com/underpass-ai/axlr-bots/internal/fakeaxlr"
)

func TestNodeRegistersRunsAndStops(t *testing.T) {
	dir := t.TempDir()
	deps, err := compose.Dependencies(filepath.Join(dir, "cp"), "/nonexistent", "test")
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "cp.sock")
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- unixapi.NewServer(unixapi.NewLocal(deps)).Serve(ctx, socket) }()
	defer func() { cancel(); <-served }()
	for !unixapi.Reachable(context.Background(), socket) {
		time.Sleep(10 * time.Millisecond)
	}
	bin, _, _ := fakeaxlr.Write(dir, fakeaxlr.Options{Scenario: "completed"})
	// The host has OpenRouter's key, a Codex proxy listening and two local
	// models in its AXLR settings: the node names them, and only them.
	t.Setenv("HOME", dir)
	t.Setenv(openRouterKeyEnv, "sk-or-test")
	t.Setenv(codexProxyKeyEnv, "")
	t.Setenv("TYPESAFE_API_KEY", "")
	// The team channel switched on from the environment, as a unit's
	// drop-in does: the node reports it.
	t.Setenv("AXLR_NODE_TEAM_CHANNEL", "true")
	proxy := listenLoopback(t)
	t.Setenv(codexProxyListenEnv, proxy.Addr().String())
	_ = os.MkdirAll(filepath.Join(dir, ".config", "axlr"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".config", "axlr", "settings.json"), []byte(`{"local_models":[{"id":"codex/gpt-6.1-sol","api_key_env":"AXLR_CODEX_PROXY_KEY"},{"id":"codex/gpt-6-luna"}]}`), 0o600)
	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- run([]string{"--control", "unix://" + socket, "--node-id", "test-node", "--labels", "host=box, zone=eu", "--state", filepath.Join(dir, "node"), "--axlr-bin", bin, "--heartbeat", "50ms", "--poll", "100ms"}, &stdout, &stderr)
	}()
	client := unixapi.NewClient(socket)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if node, err := client.GetNode(context.Background(), "test-node"); err == nil && node.Status == "Online" && node.Labels["zone"] == "eu" {
			if want := []string{"openrouter", "codex", "local:codex/gpt-6.1-sol", "local:codex/gpt-6-luna"}; !slices.Equal(node.Capabilities.Providers, want) {
				t.Fatalf("providers = %v, want %v", node.Capabilities.Providers, want)
			}
			if !node.Capabilities.TeamChannel {
				t.Fatalf("the team channel is not reported: %+v", node.Capabilities)
			}
			// The node reports the build it runs (this test binary's).
			if build := (system.BuildReader{}).Running(); node.Capabilities.NodeVersion != build.Version || node.Capabilities.NodeRevision != build.Revision {
				t.Fatalf("node build %q %q, want %+v", node.Capabilities.NodeVersion, node.Capabilities.NodeRevision, build)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("node not online: %s", stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := client.RegisterBot(context.Background(), application.BotDefinition{Tenant: "t", Name: "bot", Owner: "o", Mission: "m"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(context.Background(), "bot"); err != nil {
		t.Fatal(err)
	}
	submitted, _ := client.SubmitTask(context.Background(), unixapi.SubmitRequest{BotID: "bot", Objective: "o", IdempotencyKey: "k"})
	deadline = time.Now().Add(5 * time.Second)
	for {
		task, _ := client.GetTask(context.Background(), submitted.Task.ID)
		if task.Status == "Completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task not run by the node: %+v\n%s", task, stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if execution, _ := client.GetExecution(context.Background(), submitted.Task.ID); execution.ID == "" {
		t.Log("execution lookup by task id is not a thing; fine")
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	select {
	case code := <-done:
		if code != exitOK {
			t.Errorf("exit %d: %s", code, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("node did not stop")
	}
	if !strings.Contains(stderr.String(), "registered as test-node") || !strings.Contains(stderr.String(), "team channel on but --memory-teams names no team") {
		t.Errorf("log = %s", stderr.String())
	}
}

func TestNodeUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cases := []struct {
		args []string
		code int
		text string
	}{
		{[]string{"--bogus"}, exitUsage, "flag provided but not defined"},
		{[]string{"extra"}, exitUsage, "flags only"},
		{nil, exitUsage, "--control"},
		{[]string{"--control", "unix:///tmp/x.sock", "--labels", "novalue"}, exitUsage, "key=value"},
		{[]string{"--control", "unix:///tmp/x.sock", "--state", filepath.Join(t.TempDir(), "file", "state"), "--node-id", "Bad Id"}, exitError, ""},
	}
	_ = os.WriteFile(filepath.Join(filepath.Dir(cases[4].args[5]), "file"), []byte("x"), 0o644)
	for _, tc := range cases {
		stderr.Reset()
		if code := run(tc.args, &stdout, &stderr); code != tc.code || !strings.Contains(stderr.String(), tc.text) {
			t.Errorf("%v: %d %q", tc.args, code, stderr.String())
		}
	}
	stderr.Reset()
	if code := run([]string{"--help"}, &stdout, &stderr); code != exitOK || !strings.Contains(stderr.String(), "-team-channel") || !strings.Contains(stderr.String(), "AXLR_NODE_TEAM_CHANNEL=true; default: off") {
		t.Errorf("--help = %d %s", code, stderr.String())
	}
	if code := run([]string{"--control", "unix:///tmp/x.sock", "--state", t.TempDir(), "--node-id", "Bad Id"}, &stdout, &stderr); code != exitError || !strings.Contains(stderr.String(), "node id") {
		t.Errorf("bad node id: %d %s", code, stderr.String())
	}
	labels, err := parseLabels(" a=1 , b = 2 ,")
	if err != nil || labels["a"] != "1" || labels["b"] != "2" {
		t.Errorf("labels = %v %v", labels, err)
	}
	if defaultNodeID() == "" || defaultStateDir() == "" || envOr("AXLR_BOTS_NOPE", "x") != "x" {
		t.Error("defaults wrong")
	}
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg")
	if defaultStateDir() != "/tmp/xdg/axlr-node" {
		t.Error("XDG_STATE_HOME ignored")
	}
}

// --memory-teams takes team names or "*", and refuses what is no team.
func TestParseMemoryTeams(t *testing.T) {
	if teams, err := parseMemoryTeams(""); err != nil || teams != nil {
		t.Fatalf("empty: %v %v", teams, err)
	}
	if teams, err := parseMemoryTeams(" qa , dev ,"); err != nil || strings.Join(teams, "|") != "qa|dev" {
		t.Fatalf("names: %v %v", teams, err)
	}
	if teams, err := parseMemoryTeams("*"); err != nil || strings.Join(teams, "|") != "*" {
		t.Fatalf("star: %v %v", teams, err)
	}
	if _, err := parseMemoryTeams("qa,Not A Team"); err == nil || !strings.Contains(err.Error(), "--memory-teams") {
		t.Fatalf("bad name: %v", err)
	}
}
