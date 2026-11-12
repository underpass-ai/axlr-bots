package activityaudit

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/axlr"
	"github.com/underpass-ai/axlr-bots/adapters/nodeagent"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/compose"
	"github.com/underpass-ai/axlr-bots/domain"
)

// runLines is what the stand-in for axlr-tui writes for every run: two
// failed runs of one program with other ids, paths and shas, then a
// refusal of a host tool with a secret in it.
func runLines() string {
	secret := "ghp_" + strings.Repeat("Z", 36)
	lines := []map[string]any{
		{"type": "session", "session": "sess-1"},
		{"type": "tool.requested", "tool": "local_exec", "call_id": "c1", "arguments": map[string]any{"program": "/home/gx10a/bots/arreglo", "args": []string{"plan", "task-0dc89689b19a575f87602c94"}}},
		{"type": "tool.completed", "tool": "local_exec", "call_id": "c1", "decision": "auto_approve", "status": "failed", "error": execFailed("c1", 2, "arreglo: no plan for task-0dc89689b19a575f87602c94 in /home/gx10a/x (head 1a2b3c4d)")},
		{"type": "tool.requested", "tool": "local_exec", "call_id": "c2", "arguments": map[string]any{"program": "/home/tirso/bin/arreglo", "args": []string{"plan", "task-5dbfaeddfdb4f706d3a45963"}}},
		{"type": "tool.completed", "tool": "local_exec", "call_id": "c2", "decision": "auto_approve", "status": "failed", "error": execFailed("c2", 2, "arreglo: no plan for task-5dbfaeddfdb4f706d3a45963 in /home/tirso/y (head 9f8e7d6c)")},
		{"type": "tool.requested", "tool": "local_read", "call_id": "c3", "arguments": map[string]any{"path": "notes.md"}},
		{"type": "tool.completed", "tool": "local_read", "call_id": "c3", "decision": "auto_approve", "status": "failed", "error": `{"call_id":"c3","error":"path \"notes.md\" is outside the workspace (token ` + secret + `)"}`},
		{"type": "completed", "session": "sess-1"},
		{"type": "result", "session": "sess-1", "result": map[string]any{"schema": "axlr.execution-result/v1", "status": "completed", "session_id": "sess-1", "sessions": []string{"sess-1"}, "answer": "No plan.", "usage": map[string]any{"requests": 1},
			"tool_calls": []map[string]any{{"call_id": "c1", "tool": "local_exec", "decision": "auto_approve", "status": "failed"}, {"call_id": "c2", "tool": "local_exec", "decision": "auto_approve", "status": "failed"}, {"call_id": "c3", "tool": "local_read", "decision": "auto_approve", "status": "failed"}}}},
	}
	var b strings.Builder
	for _, line := range lines {
		raw, _ := json.Marshal(line)
		b.Write(raw)
		b.WriteByte('\n')
	}
	return b.String()
}

// standIn writes a stand-in for axlr-tui that answers its help and
// version and writes runLines for every run.
func standIn(t *testing.T, dir string) string {
	t.Helper()
	lines := filepath.Join(dir, "run.jsonl")
	if err := os.WriteFile(lines, []byte(runLines()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "fake-axlr-tui")
	script := "#!/bin/sh\nfor a in \"$@\"; do if [ \"$a\" = --help ]; then echo '  -exec string' >&2; echo '  -policy string' >&2; exit 0; fi; if [ \"$a\" = --version ]; then echo 'axlr-tui 0.0.0-fake'; exit 0; fi; done\ncat '" + lines + "'\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// The adapter against a real control plane on its socket and a real node
// that runs the stand-in: a dev bot's task ends with two failed runs of
// one program and a host tool's refusal; the adapter publishes two
// actividad.fallo to team qa as the operator, the secret redacted; a
// second task like it publishes nothing and grows the counts; the same
// publish again answers the event published before.
func TestEndToEndThroughTheDaemon(t *testing.T) {
	dir := t.TempDir()
	deps, err := compose.Dependencies(filepath.Join(dir, "cp"), "/nonexistent/axlr-tui", "test:cp")
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "cp.sock")
	serveCtx, stopServe := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- unixapi.NewServer(unixapi.NewLocal(deps)).Serve(serveCtx, socket) }()
	t.Cleanup(func() { stopServe(); <-served })
	eventually(t, "the control plane", func() bool { return unixapi.Reachable(context.Background(), socket) })
	client := unixapi.NewClient(socket)
	ctx := context.Background()

	logs := &syncBuffer{}
	agent := nodeagent.Agent{Transport: client, Executor: axlr.NewExecutor(standIn(t, dir), filepath.Join(dir, "node")), Options: nodeagent.Options{NodeID: "web-1", HostName: "web-1", Heartbeat: 50 * time.Millisecond, PollWait: 100 * time.Millisecond, Renew: 30 * time.Millisecond, ProgressEvery: 20 * time.Millisecond, Log: log.New(logs, "", 0)}}
	agentCtx, stopAgent := context.WithCancel(context.Background())
	agentDone := make(chan struct{})
	go func() { _ = agent.Run(agentCtx); close(agentDone) }()
	t.Cleanup(func() { stopAgent(); <-agentDone })

	for _, bot := range []application.BotDefinition{
		{Tenant: "default", Name: "dev-a", Owner: "o", Mission: "fix what QA finds", AllowedTools: []string{"local_read", "local_exec"}, Groups: []string{"dev"}, Budgets: application.BudgetsDefinition{TaskTimeoutSeconds: 30}},
		{Tenant: "default", Name: "qa-a", Owner: "o", Mission: "find what is broken", AllowedTools: []string{"local_read"}, Groups: []string{"qa"}, Budgets: application.BudgetsDefinition{TaskTimeoutSeconds: 30}},
	} {
		if _, err := client.RegisterBot(ctx, bot); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.EnableBot(ctx, "dev-a"); err != nil {
		t.Fatal(err)
	}

	storeDir := filepath.Join(dir, "activity-audit")
	store, err := OpenSeenStore(filepath.Join(storeDir, SeenFileName))
	if err != nil {
		t.Fatal(err)
	}
	auditor, err := NewAuditor(AuditorConfig{Source: client, Publisher: client, Store: store, Team: DefaultTeam, IgnorePrograms: DefaultIgnorePrograms, IgnoreTeamPartial: true, Log: log.New(logs, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	watcher, err := NewWatcher(WatcherConfig{Source: client, Auditor: auditor, Wait: time.Second, Log: log.New(logs, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	watchCtx, stopWatch := context.WithCancel(context.Background())
	watched := make(chan error, 1)
	go func() { watched <- watcher.Run(watchCtx, time.Time{}) }()
	eventually(t, "the watcher follows", func() bool { return strings.Contains(logs.String(), "following 2 teams") })

	published := func() []domain.Event {
		events, err := client.ListGroupEvents(ctx, application.GroupEventsQuery{Name: "qa", Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		var fallos []domain.Event
		for _, event := range events {
			if event.Type == domain.EventGroupEventPublished && event.Payload["name"] == EventName {
				fallos = append(fallos, event)
			}
		}
		return fallos
	}
	submit := func(key string) {
		t.Helper()
		submitted, err := client.SubmitTask(ctx, unixapi.SubmitRequest{BotID: "dev-a", Objective: "plan the fix", IdempotencyKey: key})
		if err != nil {
			t.Fatal(err)
		}
		eventually(t, "task "+key+" ends", func() bool {
			task, err := client.GetTask(ctx, submitted.Task.ID)
			return err == nil && task.Status == "Completed"
		})
	}

	submit("first")
	eventually(t, "two events on qa", func() bool { return len(published()) == 2 })
	events := published()
	byProgram := map[string]map[string]any{}
	for _, event := range events {
		if event.Payload["by"] != "operator" {
			t.Errorf("not the operator's: %+v", event.Payload)
		}
		data, _ := event.Payload["data"].(map[string]any)
		byProgram[strings.TrimSpace(data["tool"].(string)+" "+stringOf(data["program"]))] = data
		raw, _ := json.Marshal(event.Payload)
		if strings.Contains(string(raw), "ghp_") || strings.Contains(string(raw), "/home/") || strings.Contains(string(raw), "task-0dc8") {
			t.Errorf("the event carries what it must not: %s", raw)
		}
	}
	arreglo := byProgram["local_exec arreglo"]
	if arreglo == nil || arreglo["count"] != float64(2) || arreglo["bot"] != "dev-a" || arreglo["team"] != "dev" || !strings.HasPrefix(stringOf(arreglo["error"]), "arreglo: no plan for <id> in <path> (head <hex>)") {
		t.Fatalf("arreglo = %+v (events %+v)", arreglo, events)
	}
	if read := byProgram["local_read"]; read == nil || read["error"] != `path "…" is outside the workspace (token <secret>)` {
		t.Errorf("local_read = %+v", read)
	}

	submit("second")
	eventually(t, "the second task audited", func() bool {
		for _, s := range auditorSignatures(t, storeDir) {
			if s.Program == "arreglo" && s.Count == 4 {
				return true
			}
		}
		return false
	})
	if got := len(published()); got != 2 {
		t.Fatalf("a seen signature published again: %d", got)
	}

	// The publish's key: the same command again is the event before.
	var signature SeenSignature
	for _, s := range auditorSignatures(t, storeDir) {
		if s.Program == "arreglo" {
			signature = s
		}
	}
	again, err := client.PublishGroupEvent(ctx, auditor.command(&signature))
	if err != nil || again.Created || again.EventID != signature.EventID {
		t.Fatalf("again = %+v %v (signature %+v)", again, err, signature)
	}
	stopWatch()
	if err := <-watched; err != nil {
		t.Fatal(err)
	}
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

// auditorSignatures reads the store as a second process would.
func auditorSignatures(t *testing.T, dir string) []SeenSignature {
	t.Helper()
	store, err := OpenSeenStore(filepath.Join(dir, SeenFileName))
	if err != nil {
		t.Fatal(err)
	}
	return store.Signatures()
}
