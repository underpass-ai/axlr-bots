package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/activityaudit"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/internal/statelock"
)

func TestActivityAuditRefuses(t *testing.T) {
	state := t.TempDir()
	socket := filepath.Join(t.TempDir(), "cp.sock")
	bad := filepath.Join(t.TempDir(), "bad")
	_ = os.MkdirAll(bad, 0o700)
	_ = os.WriteFile(filepath.Join(bad, activityaudit.SeenFileName), []byte("{"), 0o600)
	locked := filepath.Join(t.TempDir(), "locked")
	release, err := statelock.Acquire(locked)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	need := []string{"--team", "watchers", "--event", "activity.failure"}
	for name, c := range map[string]struct {
		socket string
		args   []string
		code   int
		want   string
	}{
		"extra":         {socket, append([]string{"extra"}, need...), exitUsage, "takes no arguments"},
		"no team":       {socket, []string{"--event", "activity.failure"}, exitUsage, "needs --team"},
		"no event":      {socket, []string{"--team", "watchers"}, exitUsage, "--event"},
		"bad flag":      {socket, []string{"--bogus"}, exitUsage, ""},
		"bad since":     {socket, append([]string{"--since", "today"}, need...), exitError, "--since today"},
		"no socket":     {"", need, exitError, "--socket is empty"},
		"locked":        {socket, append([]string{"--store", locked}, need...), exitError, "in use by another process"},
		"bad store":     {socket, append([]string{"--store", bad}, need...), exitError, bad},
		"unknown rearm": {socket, append([]string{"--store", t.TempDir(), "--rearm", "failure-nope"}, need...), exitError, "no signature failure-nope"},
		"bad team":      {socket, []string{"--store", t.TempDir(), "--team", "Not A Team", "--event", "activity.failure"}, exitError, "team"},
		"bad event":     {socket, []string{"--store", t.TempDir(), "--team", "watchers", "--event", "Not An Event"}, exitError, "event name"},
		"daemon down":   {socket, append([]string{"--store", t.TempDir()}, need...), exitError, "list the teams"},
	} {
		var stdout, stderr bytes.Buffer
		code := activityAuditServe(context.Background(), state, c.socket, remoteControl{}, c.args, &stdout, &stderr)
		if code != c.code || !strings.Contains(stderr.String(), c.want) {
			t.Errorf("%s: exit %d, %s", name, code, stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--state", state, "--socket", socket, "activity-audit"}, &stdout, &stderr); code != exitUsage || !strings.Contains(stderr.String(), "serve or report") {
		t.Errorf("no verb: %d %s", code, stderr.String())
	}
	for _, args := range [][]string{{"extra"}, {"--bogus"}} {
		if code := activityAuditReport(state, args, &stdout, &stderr); code != exitUsage {
			t.Errorf("report %v: %d", args, code)
		}
	}
	if code := activityAuditReport(state, []string{"--store", bad}, &stdout, &stderr); code != exitError {
		t.Errorf("report of a bad store: %d", code)
	}
	stdout.Reset()
	if code := run([]string{"--state", state, "activity-audit", "report"}, &stdout, &stderr); code != exitOK || !strings.HasPrefix(stdout.String(), "ID") {
		t.Errorf("an empty report: %d %q", code, stdout.String())
	}
	if got := splitList(" a, ,b,"); strings.Join(got, "|") != "a|b" {
		t.Errorf("splitList = %v", got)
	}
}

// The adapter's commands against a real daemon: a task fails; a dry run
// of --since prints what it would publish and keeps nothing; a replay
// publishes it to team watchers as activity.failure; a second run publishes
// nothing; report prints the store; --rearm clears a signature.
func TestActivityAuditServeAgainstTheDaemon(t *testing.T) {
	state := t.TempDir()
	socket := filepath.Join(t.TempDir(), "cp.sock")
	c := cli{t: t, state: state, axlr: fakeAXLR(t, "failed")}
	raw, err := os.ReadFile(filepath.Join("testdata", "bot-team-a.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, bot := range []struct{ id, group string }{{"worker-a", "workers"}, {"watcher-a", "watchers"}} {
		definition := filepath.Join(t.TempDir(), bot.id+".json")
		_ = os.WriteFile(definition, bytes.ReplaceAll(bytes.ReplaceAll(raw, []byte(`"releases"`), []byte(`"`+bot.group+`"`)), []byte(`"team-a"`), []byte(`"`+bot.id+`"`)), 0o644)
		if code, _, errText := c.run("bot", "register", "--file", definition); code != exitOK {
			t.Fatalf("register: %s", errText)
		}
	}
	daemonCtx, stopDaemon := context.WithCancel(context.Background())
	daemonDone := make(chan int, 1)
	var daemonOut, daemonErr syncBuffer
	go func() {
		daemonDone <- serve(daemonCtx, mustApp(t, state, c.axlr), state, socket, nil, &daemonOut, &daemonErr)
	}()
	defer func() { stopDaemon(); <-daemonDone }()
	for !unixapi.Reachable(context.Background(), socket) {
		time.Sleep(10 * time.Millisecond)
	}
	client := unixapi.NewClient(socket)
	ctx := context.Background()
	since := time.Now().UTC().Add(-time.Second)
	if _, err := client.EnableBot(ctx, "worker-a"); err != nil {
		t.Fatal(err)
	}
	submitted, err := client.SubmitTask(ctx, unixapi.SubmitRequest{BotID: "worker-a", Objective: "plan the fix", IdempotencyKey: "k-1"})
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; {
		task, err := client.GetTask(ctx, submitted.Task.ID)
		if err == nil && task.Status == "Failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %+v %v\n%s", task, err, daemonErr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	store := filepath.Join(t.TempDir(), "audit")
	serveOnce := func(extra ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		args := append([]string{"--team", "watchers", "--event", "activity.failure", "--store", store, "--once", "--since", since.Format(time.RFC3339Nano)}, extra...)
		code := activityAuditServe(ctx, state, socket, remoteControl{}, args, &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}
	published := func() []domain.Event {
		events, err := client.ListGroupEvents(ctx, application.GroupEventsQuery{Name: "watchers", Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		var failures []domain.Event
		for _, event := range events {
			if event.Type == domain.EventGroupEventPublished && event.Payload["name"] == "activity.failure" {
				failures = append(failures, event)
			}
		}
		return failures
	}

	code, out, errText := serveOnce("--dry-run")
	if code != exitOK || !strings.Contains(out, "would publish 1 of 1 signatures") || !strings.Contains(out, "task failed") || len(published()) != 0 {
		t.Fatalf("dry run: %d\n%s\n%s", code, out, errText)
	}
	if _, err := os.Stat(filepath.Join(store, activityaudit.SeenFileName)); !os.IsNotExist(err) {
		t.Errorf("the dry run kept a store: %v", err)
	}

	if code, _, errText := serveOnce(); code != exitOK || len(published()) != 1 {
		t.Fatalf("replay: %d %s, published %d", code, errText, len(published()))
	}
	event := published()[0]
	data, _ := event.Payload["data"].(map[string]any)
	if event.Payload["by"] != "operator" || data["kind"] != activityaudit.KindTaskFailed || data["bot"] != "worker-a" || data["team"] != "workers" || data["task"] != submitted.Task.ID {
		t.Errorf("event = %+v", event.Payload)
	}
	if code, _, errText := serveOnce(); code != exitOK || len(published()) != 1 || !strings.Contains(errText, "--since ignored") {
		t.Fatalf("second run: %d %s, published %d", code, errText, len(published()))
	}

	var stdout, stderr bytes.Buffer
	if code := activityAuditReport(state, []string{"--store", store}, &stdout, &stderr); code != exitOK || !strings.Contains(stdout.String(), "task failed") {
		t.Errorf("report: %d %s", code, stdout.String())
	}
	stdout.Reset()
	if code := activityAuditReport(state, []string{"--store", store, "--json"}, &stdout, &stderr); code != exitOK || !strings.Contains(stdout.String(), `"event_id"`) {
		t.Errorf("report --json: %d %s", code, stdout.String())
	}
	if code, _, errText := serveOnce("--rearm", "all"); code != exitOK || !strings.Contains(errText, "rearmed failure-") {
		t.Errorf("rearm: %d %s", code, errText)
	}
}
