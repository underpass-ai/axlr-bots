//go:build sqlite

package sqlitestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func record(execution, session string) application.TranscriptRecord {
	return application.TranscriptRecord{Execution: domain.ExecutionID(execution), Task: "task-1", Bot: "worker-1", Session: session, Size: 3, SHA256: "abc", ReceivedAt: time.Date(2026, 11, 13, 10, 0, 0, 123, time.UTC)}
}

func TestSaveLoadListReplace(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := NewTranscriptStore(dir)
	t.Cleanup(func() { _ = store.Close() })

	first, err := store.SaveTranscript(ctx, record("exec-1", "s1"), []byte(`{"a":1}`))
	if err != nil || first.Seq != 1 {
		t.Fatalf("save: %+v %v", first, err)
	}
	if _, err := store.SaveTranscript(ctx, record("exec-2", "s2"), nil); err != nil {
		t.Fatal(err)
	}
	got, content, err := store.LoadTranscript(ctx, "exec-1", "s1")
	if err != nil || string(content) != `{"a":1}` || got.Bot != "worker-1" || got.Task != "task-1" || !got.ReceivedAt.Equal(record("", "").ReceivedAt) {
		t.Fatalf("load: %+v %q %v", got, content, err)
	}
	filtered := record("exec-3", "s3")
	filtered.Filtered, filtered.SentSHA256 = true, "sent"
	if _, err := store.SaveTranscript(ctx, filtered, nil); err != nil {
		t.Fatal(err)
	}
	if got, _, err := store.LoadTranscript(ctx, "exec-3", "s3"); err != nil || !got.Filtered || got.SentSHA256 != "sent" {
		t.Fatalf("filtered: %+v %v", got, err)
	}
	// A replaced transcript comes again after the cursor.
	again, err := store.SaveTranscript(ctx, record("exec-1", "s1"), []byte(`{"a":2}`))
	if err != nil || again.Seq != 4 {
		t.Fatalf("replace: %+v %v", again, err)
	}
	list, err := store.ListTranscripts(ctx, 0, 10)
	if err != nil || len(list) != 3 || list[0].Execution != "exec-2" || list[2].Seq != 4 {
		t.Fatalf("list: %+v %v", list, err)
	}
	after, err := store.ListTranscripts(ctx, 3, 10)
	if err != nil || len(after) != 1 || after[0].Execution != "exec-1" {
		t.Fatalf("after: %+v %v", after, err)
	}
	limited, err := store.ListTranscripts(ctx, 0, 1)
	if err != nil || len(limited) != 1 {
		t.Fatalf("limit: %+v %v", limited, err)
	}
	if _, _, err := store.LoadTranscript(ctx, "exec-9", "s1"); !errors.As(err, &domain.NotFoundError{}) {
		t.Fatalf("missing: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, TranscriptFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file: %v %v", info, err)
	}
	// A second store over the same file reads what the first wrote.
	other := NewTranscriptStore(dir)
	t.Cleanup(func() { _ = other.Close() })
	if _, content, err := other.LoadTranscript(ctx, "exec-1", "s1"); err != nil || string(content) != `{"a":2}` {
		t.Fatalf("reopen: %q %v", content, err)
	}
}

func TestOpenFailsOnAMissingDirectory(t *testing.T) {
	store := NewTranscriptStore(filepath.Join(t.TempDir(), "absent"))
	ctx := context.Background()
	if _, err := store.SaveTranscript(ctx, record("exec-1", "s1"), nil); err == nil {
		t.Fatal("save in a missing directory")
	}
	if _, _, err := store.LoadTranscript(ctx, "exec-1", "s1"); err == nil {
		t.Fatal("load in a missing directory")
	}
	if _, err := store.ListTranscripts(ctx, 0, 1); err == nil {
		t.Fatal("list in a missing directory")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestABadTimestampIsAnError(t *testing.T) {
	ctx := context.Background()
	store := NewTranscriptStore(t.TempDir())
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.SaveTranscript(ctx, record("exec-1", "s1"), nil); err != nil {
		t.Fatal(err)
	}
	db, _ := store.open()
	if _, err := db.Exec(`UPDATE transcripts SET received_at = 'yesterday'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.LoadTranscript(ctx, "exec-1", "s1"); err == nil {
		t.Fatal("bad timestamp loaded")
	}
	if _, err := store.ListTranscripts(ctx, 0, 1); err == nil {
		t.Fatal("bad timestamp listed")
	}
}

func TestCancelledContext(t *testing.T) {
	store := NewTranscriptStore(t.TempDir())
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.SaveTranscript(ctx, record("exec-1", "s1"), nil); err == nil {
		t.Fatal("save with a cancelled context")
	}
	if _, err := store.ListTranscripts(ctx, 0, 1); err == nil {
		t.Fatal("list with a cancelled context")
	}
}
