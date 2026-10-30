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

func TestEventLogListsATeamsEvents(t *testing.T) {
	ctx := context.Background()
	log := NewEventLog(t.TempDir())
	for i, bot := range []domain.BotID{"plane", "stranger", "quad-1", "plane"} {
		event, _ := domain.NewEvent(domain.EventID("evt-"+string(rune('a'+i))), domain.EventTaskSubmitted, "underpass", bot, "task-1", "trace", t0, nil)
		_ = log.Publish(ctx, event)
	}
	events, err := log.ListTeamEvents(ctx, "default", "port", []domain.BotID{"plane", "quad-1"}, 10)
	if err != nil || len(events) != 3 || events[0].ID != "evt-a" || events[1].ID != "evt-c" || events[2].ID != "evt-d" {
		t.Errorf("team = %+v %v", events, err)
	}
	if newest, _ := log.ListTeamEvents(ctx, "default", "port", []domain.BotID{"plane", "quad-1"}, 2); len(newest) != 2 || newest[0].ID != "evt-c" {
		t.Errorf("newest = %+v", newest)
	}
	if none, _ := log.ListTeamEvents(ctx, "default", "none", nil, 10); len(none) != 0 {
		t.Errorf("nobody = %+v", none)
	}
	// The operator's events on the team are in its timeline; another
	// tenant's team of that name is not.
	for i, tenant := range []domain.TenantID{"underpass", "other"} {
		event, err := domain.NewGroupEvent(domain.EventID("evt-g"+string(rune('a'+i))), domain.EventGroupEventPublished, tenant, "port", t0, nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = log.Publish(ctx, event)
	}
	if team, _ := log.ListTeamEvents(ctx, "underpass", "port", []domain.BotID{"plane"}, 10); len(team) != 3 || team[2].ID != "evt-ga" {
		t.Errorf("with the operator's = %+v", team)
	}
}

// A line longer than a read takes is skipped, as before the index, and
// a caller's limit never sizes an allocation: a huge limit reads what
// the log holds.
func TestEventLogBoundsItsAllocations(t *testing.T) {
	ctx := context.Background()
	log := NewEventLog(t.TempDir())
	event, _ := domain.NewEvent("evt-a", domain.EventTaskSubmitted, "underpass", "bot-1", "task-1", "trace", t0, nil)
	if err := log.Publish(ctx, event); err != nil {
		t.Fatal(err)
	}
	f, _ := os.OpenFile(log.Path(), os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString(`{"eventId": "evt-long", "schemaVersion": "underpass.ai/events/v1", "type": "bot.task.submitted", "tenantId": "underpass", "botId": "bot-1", "createdAt": "2026-10-23T15:00:00Z", "payload": {"x": "` + strings.Repeat("x", maxEventLineBytes) + `"}}` + "\n")
	_ = f.Close()
	event, _ = domain.NewEvent("evt-b", domain.EventTaskSubmitted, "underpass", "bot-1", "task-1", "trace", t0, nil)
	_ = log.Publish(ctx, event)
	events, err := log.ListEvents(ctx, "bot-1", 1<<30)
	if err != nil || len(events) != 2 || events[0].ID != "evt-a" || events[1].ID != "evt-b" {
		t.Fatalf("events = %v %v", events, err)
	}
}

// The index a read keeps follows the log: lines another process
// appends, a torn line once it is whole, a line longer than the read
// buffer, a line that is not an event, and a log that was replaced.
func TestEventLogIndexFollowsTheLog(t *testing.T) {
	ctx := context.Background()
	state := t.TempDir()
	log := NewEventLog(state)
	publish := func(l *EventLog, id string, payload map[string]any) {
		t.Helper()
		event, _ := domain.NewEvent(domain.EventID(id), domain.EventTaskSubmitted, "underpass", "bot-1", "task-1", "trace", t0, payload)
		if err := l.Publish(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	ids := func() []string {
		t.Helper()
		events, err := log.ListEvents(ctx, "bot-1", 100)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, len(events))
		for i, event := range events {
			out[i] = string(event.ID)
		}
		return out
	}
	publish(log, "evt-a", nil)
	if got := ids(); strings.Join(got, ",") != "evt-a" {
		t.Fatalf("first = %v", got)
	}
	// Another process appends: the next read sees it.
	publish(NewEventLog(state), "evt-b", map[string]any{"long": strings.Repeat("x", 600*1024)})
	f, _ := os.OpenFile(log.Path(), os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString("not an event\n")
	_, _ = f.WriteString(`{"eventId": "evt-c", "schemaVersion": "underpass.ai/events/v1", "type": "bot.task.submitted", "tenantId": "underpass", "botId": "bot-1", `)
	_ = f.Close()
	if got := ids(); strings.Join(got, ",") != "evt-a,evt-b" {
		t.Fatalf("with a long line, a bad one and a torn one = %v", got)
	}
	// The torn line is finished: it is read now.
	f, _ = os.OpenFile(log.Path(), os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString(`"createdAt": "2026-10-23T15:00:00Z"}` + "\n")
	_ = f.Close()
	if got := ids(); strings.Join(got, ",") != "evt-a,evt-b,evt-c" {
		t.Fatalf("finished = %v", got)
	}
	if events, _ := log.ListEvents(ctx, "bot-1", 3); len(events[1].Payload["long"].(string)) != 600*1024 {
		t.Error("the long line is not read whole")
	}
	// The log is replaced by a shorter one: it is indexed again.
	if err := os.WriteFile(log.Path(), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	publish(log, "evt-z", nil)
	if got := ids(); strings.Join(got, ",") != "evt-z" {
		t.Fatalf("after the log was replaced = %v", got)
	}
}
