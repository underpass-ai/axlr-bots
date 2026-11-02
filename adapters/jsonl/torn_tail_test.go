package jsonl

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

// TestEventLogKeepsTheEventAfterATornTail: a crash left half a line;
// the next event goes on a line of its own and is read, and the logs
// are not readable by others.
func TestEventLogKeepsTheEventAfterATornTail(t *testing.T) {
	ctx := context.Background()
	state := t.TempDir()
	log := NewEventLog(state)
	publish := func(id string) {
		t.Helper()
		event, _ := domain.NewEvent(domain.EventID(id), domain.EventTaskSubmitted, "underpass", "bot-1", "task-1", "trace", t0, nil)
		if err := log.Publish(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	publish("evt-a")
	f, _ := os.OpenFile(log.Path(), os.O_APPEND|os.O_WRONLY, 0o640)
	_, _ = f.WriteString(`{"eventId": "evt-torn", "botId": "bot-1"`)
	_ = f.Close()
	publish("evt-b")
	publish("evt-c")
	events, err := log.ListEvents(ctx, "bot-1", 10)
	if err != nil || len(events) != 3 || events[0].ID != "evt-a" || events[1].ID != "evt-b" || events[2].ID != "evt-c" {
		t.Errorf("events after a torn tail = %+v %v", events, err)
	}
	audit := NewAuditLog(state)
	entry, _ := domain.NewAuditEntry("aud-1", t0, "cli:me", "bot.register", "bot-1", domain.AuditOK, nil)
	if err := audit.Record(ctx, entry); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{log.Path(), audit.Path()} {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm()&0o007 != 0 {
			t.Errorf("%s mode = %v %v", path, info.Mode().Perm(), err)
		}
	}
}

func TestAfterTornTailFailsOnAClosedFile(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "x.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if _, err := afterTornTail(f, []byte("{}\n")); err == nil {
		t.Error("a closed file was read")
	}
}
