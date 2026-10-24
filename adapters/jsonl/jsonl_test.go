package jsonl

import (
	"bufio"
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

var (
	_ application.EventPublisher = (*EventLog)(nil)
	_ application.AuditSink      = (*AuditLog)(nil)
)

var t0 = time.Date(2026, 10, 23, 15, 0, 0, 0, time.UTC)

func lines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []map[string]any
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("line %q: %v", scanner.Text(), err)
		}
		out = append(out, record)
	}
	return out
}

func TestEventLogAppends(t *testing.T) {
	ctx := context.Background()
	state := t.TempDir()
	log := NewEventLog(state)
	if log.Path() != filepath.Join(state, "events.jsonl") {
		t.Errorf("path = %s", log.Path())
	}
	for i, kind := range []domain.EventType{domain.EventBotRegistered, domain.EventTaskSubmitted} {
		event, _ := domain.NewEvent(domain.EventID("evt-"+string(rune('a'+i))), kind, "underpass", "bot-1", "task-1", "trace", t0, map[string]any{"i": i})
		if err := log.Publish(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	// A second log over the same file keeps appending: nothing is lost on restart.
	event, _ := domain.NewEvent("evt-c", domain.EventBotEnabled, "underpass", "bot-1", "", "", t0, nil)
	if err := NewEventLog(state).Publish(ctx, event); err != nil {
		t.Fatal(err)
	}
	got := lines(t, log.Path())
	if len(got) != 3 || got[0]["type"] != "bot.registered" || got[1]["type"] != "bot.task.submitted" || got[2]["type"] != "bot.enabled" {
		t.Errorf("lines = %v", got)
	}
	if got[0]["schemaVersion"] != string(domain.EventSchemaV1) || got[0]["eventId"] != "evt-a" {
		t.Errorf("first line = %v", got[0])
	}
	if err := log.Publish(ctx, domain.Event{}); err == nil {
		t.Error("invalid event appended")
	}
}

func TestAuditLogAppends(t *testing.T) {
	ctx := context.Background()
	log := NewAuditLog(t.TempDir())
	entry, _ := domain.NewAuditEntry("aud-1", t0, "cli:me", "bot.register", "bot-1", domain.AuditOK, map[string]string{"k": "v"})
	if err := log.Record(ctx, entry); err != nil {
		t.Fatal(err)
	}
	got := lines(t, log.Path())
	if len(got) != 1 || got[0]["action"] != "bot.register" || got[0]["outcome"] != "ok" || !strings.HasSuffix(log.Path(), "audit.jsonl") {
		t.Errorf("lines = %v", got)
	}
}

func TestAppenderFailures(t *testing.T) {
	state := t.TempDir()
	blocker := filepath.Join(state, "blocker")
	_ = os.WriteFile(blocker, []byte("x"), 0o644)
	a := &appender{path: filepath.Join(blocker, "events.jsonl")}
	if err := a.append(map[string]int{"a": 1}); err == nil {
		t.Error("append under a file succeeded")
	}
	dir := &appender{path: state}
	if err := dir.append(map[string]int{"a": 1}); err == nil {
		t.Error("append to a directory succeeded")
	}
	if err := (&appender{path: filepath.Join(state, "x.jsonl")}).append(make(chan int)); err == nil {
		t.Error("channel marshalled")
	}
}

func TestEventLogListsABotsEvents(t *testing.T) {
	ctx := context.Background()
	state := t.TempDir()
	log := NewEventLog(state)
	var _ application.EventReader = log
	if events, err := log.ListEvents(ctx, "bot-1", 10); err != nil || len(events) != 0 {
		t.Error("a missing log should list nothing:", err)
	}
	for i := range 5 {
		bot := domain.BotID("bot-1")
		if i == 2 {
			bot = "bot-2"
		}
		event, _ := domain.NewEvent(domain.EventID("evt-"+string(rune('a'+i))), domain.EventTaskSubmitted, "underpass", bot, "task-1", "trace", t0, map[string]any{"i": i})
		event.ExecutionID = "exec-1"
		_ = log.Publish(ctx, event)
	}
	// A torn last line is skipped, not fatal.
	f, _ := os.OpenFile(log.Path(), os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString(`{"eventId": "evt-torn", "botId": "bot-1"`)
	_ = f.Close()
	events, err := log.ListEvents(ctx, "bot-1", 10)
	if err != nil || len(events) != 4 || events[0].ID != "evt-a" || events[3].ID != "evt-e" || events[0].ExecutionID != "exec-1" {
		t.Errorf("events = %+v %v", events, err)
	}
	newest, _ := log.ListEvents(ctx, "bot-1", 2)
	if len(newest) != 2 || newest[0].ID != "evt-d" || newest[1].ID != "evt-e" {
		t.Errorf("newest two = %+v", newest)
	}
	if one, _ := log.ListEvents(ctx, "bot-2", 0); len(one) != 1 || one[0].ID != "evt-c" {
		t.Errorf("bot-2 = %+v", one)
	}
	if _, err := (&EventLog{file: &appender{path: state}}).ListEvents(ctx, "bot-1", 1); err == nil {
		t.Error("a directory read as a log")
	}
}

func TestEventLogListsATasksEvents(t *testing.T) {
	ctx := context.Background()
	log := NewEventLog(t.TempDir())
	if events, err := log.ListTaskEvents(ctx, "task-1", 10); err != nil || len(events) != 0 {
		t.Error("a missing log should list nothing:", err)
	}
	for i, task := range []domain.TaskID{"task-1", "task-2", "task-1", "task-1"} {
		event, _ := domain.NewEvent(domain.EventID("evt-"+string(rune('a'+i))), domain.EventTaskSubmitted, "underpass", "bot-1", task, "trace", t0, nil)
		_ = log.Publish(ctx, event)
	}
	events, err := log.ListTaskEvents(ctx, "task-1", 10)
	if err != nil || len(events) != 3 || events[0].ID != "evt-a" || events[2].ID != "evt-d" {
		t.Errorf("task-1 = %+v %v", events, err)
	}
	if newest, _ := log.ListTaskEvents(ctx, "task-1", 1); len(newest) != 1 || newest[0].ID != "evt-d" {
		t.Errorf("newest = %+v", newest)
	}
	if other, _ := log.ListTaskEvents(ctx, "task-2", 0); len(other) != 1 || other[0].ID != "evt-b" {
		t.Errorf("task-2 = %+v", other)
	}
}
