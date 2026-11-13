package filestore

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

func transcriptRecord(execution, session string) application.TranscriptRecord {
	return application.TranscriptRecord{Execution: domain.ExecutionID(execution), Task: "task-1", Bot: "dev-go", Session: session, Size: 3, SHA256: "abc", ReceivedAt: time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)}
}

func TestTranscriptStoreOnFiles(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := NewTranscriptStore(dir)
	if list, err := store.ListTranscripts(ctx, 0, 10); err != nil || len(list) != 0 {
		t.Fatalf("empty: %+v %v", list, err)
	}
	first, err := store.SaveTranscript(ctx, transcriptRecord("exec-1", "s1"), []byte(`{"a":1}`))
	if err != nil || first.Seq != 1 {
		t.Fatalf("save: %+v %v", first, err)
	}
	if _, err := store.SaveTranscript(ctx, transcriptRecord("exec-2", "s2"), nil); err != nil {
		t.Fatal(err)
	}
	again, err := store.SaveTranscript(ctx, transcriptRecord("exec-1", "s1"), []byte(`{"a":2}`))
	if err != nil || again.Seq != 3 {
		t.Fatalf("replace: %+v %v", again, err)
	}
	// A new store over the same files sees what the first wrote.
	other := NewTranscriptStore(dir)
	got, content, err := other.LoadTranscript(ctx, "exec-1", "s1")
	if err != nil || got.Seq != 3 || string(content) != `{"a":2}` || got.Bot != "dev-go" {
		t.Fatalf("load: %+v %q %v", got, content, err)
	}
	list, err := other.ListTranscripts(ctx, 0, 10)
	if err != nil || len(list) != 2 || list[0].Execution != "exec-2" || list[1].Seq != 3 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if list, _ := other.ListTranscripts(ctx, 0, 1); len(list) != 1 {
		t.Errorf("limit: %+v", list)
	}
	if next, err := other.SaveTranscript(ctx, transcriptRecord("exec-3", "s3"), nil); err != nil || next.Seq != 4 {
		t.Fatalf("seq after reopen: %+v %v", next, err)
	}
	if _, _, err := other.LoadTranscript(ctx, "exec-9", "s1"); !domain.IsNotFound(err) {
		t.Errorf("missing: %v", err)
	}
	for _, bad := range [][2]string{{"../x", "s"}, {"exec-1", "a/b"}, {"", "s"}} {
		if _, err := other.SaveTranscript(ctx, transcriptRecord(bad[0], bad[1]), nil); err == nil {
			t.Errorf("%v saved", bad)
		}
		if _, _, err := other.LoadTranscript(ctx, domain.ExecutionID(bad[0]), bad[1]); err == nil {
			t.Errorf("%v loaded", bad)
		}
	}
	// A torn last line is skipped.
	index, _ := os.OpenFile(filepath.Join(dir, "transcripts", "index.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	_, _ = index.WriteString(`{"seq": 9, "torn`)
	_ = index.Close()
	if list, err := NewTranscriptStore(dir).ListTranscripts(ctx, 0, 10); err != nil || len(list) != 3 {
		t.Errorf("torn: %+v %v", list, err)
	}
	if info, _ := os.Stat(filepath.Join(dir, "transcripts", "exec-1", "s1.json")); info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", info.Mode())
	}
}

func TestTranscriptStoreFailures(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	// The index is a directory: nothing reads or writes.
	_ = os.MkdirAll(filepath.Join(dir, "transcripts", "index.jsonl"), 0o700)
	store := NewTranscriptStore(dir)
	if _, err := store.SaveTranscript(ctx, transcriptRecord("exec-1", "s1"), nil); err == nil {
		t.Error("saved with a broken index")
	}
	if _, err := store.ListTranscripts(ctx, 0, 1); err == nil {
		t.Error("listed with a broken index")
	}
	if _, _, err := store.LoadTranscript(ctx, "exec-1", "s1"); err == nil {
		t.Error("loaded with a broken index")
	}
	// A record whose file is gone.
	ok := NewTranscriptStore(t.TempDir())
	if _, err := ok.SaveTranscript(ctx, transcriptRecord("exec-1", "s1"), nil); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(ok.dir, "exec-1", "s1.json"))
	if _, _, err := ok.LoadTranscript(ctx, "exec-1", "s1"); err == nil || errors.As(err, &domain.NotFoundError{}) {
		t.Errorf("gone file: %v", err)
	}
	// A content directory that cannot be made.
	blocked := NewTranscriptStore(t.TempDir())
	_ = os.MkdirAll(blocked.dir, 0o700)
	_ = os.WriteFile(filepath.Join(blocked.dir, "exec-1"), nil, 0o600)
	if _, err := blocked.SaveTranscript(ctx, transcriptRecord("exec-1", "s1"), nil); err == nil {
		t.Error("saved under a file")
	}
}
