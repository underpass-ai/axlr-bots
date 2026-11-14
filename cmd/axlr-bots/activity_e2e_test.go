package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// delegatingBotFile writes a bot definition that delegates to worker, or
// the worker itself, taking delegated tasks.
func delegatingBotFile(t *testing.T, dir, id string, delegates bool) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "bot.json"))
	if err != nil {
		t.Fatal(err)
	}
	var def map[string]any
	if err := json.Unmarshal(raw, &def); err != nil {
		t.Fatal(err)
	}
	def["id"], def["name"] = id, id
	if delegates {
		def["delegation"] = map[string]any{"to": []string{"worker"}, "maxDepth": 1, "budgetShare": 0.5}
	} else {
		def["triggers"] = []any{map[string]any{"type": "manual"}, map[string]any{"type": "delegation"}}
	}
	out, _ := json.Marshal(def)
	path := filepath.Join(dir, id+".json")
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestTaskActivityAgainstTheRealDaemon seeds a task that delegates to a
// bot, starts serve and reads the tree with task activity over the
// daemon's gRPC socket, as text and as --json.
func TestTaskActivityAgainstTheRealDaemon(t *testing.T) {
	state := t.TempDir()
	files := t.TempDir()
	seed := cli{t: t, state: state, axlr: "/nonexistent/axlr-tui"}
	for _, def := range []struct {
		id        string
		delegates bool
	}{{"lead", true}, {"worker", false}} {
		if code, _, errText := seed.run("bot", "register", "--file", delegatingBotFile(t, files, def.id, def.delegates)); code != exitOK {
			t.Fatalf("register %s: %s", def.id, errText)
		}
		if code, _, errText := seed.run("bot", "enable", def.id); code != exitOK {
			t.Fatalf("enable %s: %s", def.id, errText)
		}
	}
	fake := fakeAXLR(t, "completed")
	ctx := context.Background()
	deps := mustApp(t, state, fake)
	if _, err := application.NewRegisterNode(deps).Execute(ctx, application.NodeRegistration{ID: "web-1", HostName: "web-1.example", Capabilities: domain.NodeCapabilities{AXLRVersion: "0.14.0", PolicySupported: true}}); err != nil {
		t.Fatal(err)
	}
	submitted, err := application.NewSubmitTask(deps).Execute(ctx, application.SubmitTaskCommand{BotID: "lead", Objective: "Ask the worker what it knows", IdempotencyKey: "act-1"})
	if err != nil {
		t.Fatal(err)
	}
	parentID := submitted.Task.ID().String()
	assignment, found, err := application.NewNextAssignment(deps).Execute(ctx, "web-1")
	if err != nil || !found {
		t.Fatalf("next assignment: %v %v", found, err)
	}
	answer, err := application.NewDelegateTask(deps).Execute(ctx, "web-1", assignment.ExecutionID, application.DelegateCommand{Bot: "worker", Objective: "Say what you know", Wait: true})
	if err != nil || answer.Refused != nil {
		t.Fatalf("delegate: %v %+v", err, answer)
	}
	childID := answer.TaskID.String()

	socket := filepath.Join(t.TempDir(), "cp.sock")
	serveCtx, stop := context.WithCancel(ctx)
	done := make(chan int, 1)
	var serveOut, serveErr syncBuffer
	go func() {
		done <- serve(serveCtx, mustApp(t, state, fake), state, socket, []string{"--tick", "1h"}, &serveOut, &serveErr)
	}()
	defer func() { stop(); <-done }()
	for deadline := time.Now().Add(5 * time.Second); !daemonAnswers(ctx, state, socket); {
		if time.Now().After(deadline) {
			t.Fatalf("daemon not reachable: %s", serveErr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	c := cli{t: t, state: state, axlr: "/nonexistent/axlr-tui", socket: socket}

	code, text, errText := c.run("task", "activity", parentID)
	if code != exitOK || !strings.Contains(text, parentID) || !strings.Contains(text, childID) || !strings.Contains(text, "worker") {
		t.Fatalf("text: %d\n%s\n%s", code, text, errText)
	}
	code, out, errText := c.run("task", "activity", parentID, "--json")
	if code != exitOK {
		t.Fatalf("json: %d %s", code, errText)
	}
	var tree struct {
		Task struct {
			ID    string `json:"id"`
			BotID string `json:"botId"`
		} `json:"task"`
		Children []struct {
			Task struct {
				ID    string `json:"id"`
				BotID string `json:"botId"`
			} `json:"task"`
		} `json:"children"`
	}
	if err := json.Unmarshal([]byte(out), &tree); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if tree.Task.ID != parentID || len(tree.Children) != 1 || tree.Children[0].Task.ID != childID || tree.Children[0].Task.BotID != "worker" {
		t.Errorf("tree = %+v\n%s", tree, out)
	}
	if code, out, _ := c.run("task", "activity", parentID, "--depth", "1", "--json"); code != exitOK || !strings.Contains(out, childID) {
		t.Errorf("--depth 1: %d %s", code, out)
	}
	if code, _, errText := c.run("task", "activity", "ghost"); code != exitError || !strings.Contains(errText, "not found") {
		t.Errorf("unknown task: %d %s", code, errText)
	}
}
