package filestore

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

func TestDeliveryRepositoryMovesADeliveryOnceItIsDone(t *testing.T) {
	state := t.TempDir()
	repo := NewDeliveryRepository(state)
	ctx := context.Background()
	target, _ := domain.NewNotifyTarget(domain.NotifyTargetSnapshot{Type: "command", Name: "desk", Command: []string{"/bin/true"}})
	if pending, err := repo.ListPending(ctx); err != nil || len(pending) != 0 {
		t.Fatalf("empty outbox: %v %v", pending, err)
	}
	if _, err := repo.Get(ctx, "dlv-1"); !domain.IsNotFound(err) {
		t.Errorf("missing delivery: %v", err)
	}
	d, _ := domain.NewDelivery("dlv-1", "bot-1", "task-1", "exec-1", "call-1", target, json.RawMessage(`{"a":1}`), t0)
	if err := repo.Save(ctx, d); err != nil {
		t.Fatal(err)
	}
	other, _ := domain.NewDelivery("dlv-2", "bot-1", "task-1", "exec-1", "call-1", target, json.RawMessage(`{}`), t0)
	_ = repo.Save(ctx, other)
	pending, err := repo.ListPending(ctx)
	if err != nil || len(pending) != 2 || pending[0].ID() != "dlv-1" || string(pending[0].Payload()) != `{"a":1}` {
		t.Fatalf("pending = %v %v", pending, err)
	}
	_ = d.Delivered(t0.Add(time.Second))
	if err := repo.Save(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(state, "deliveries", "pending", "dlv-1.json")); !errors.Is(err, os.ErrNotExist) {
		t.Error("a delivered delivery stayed pending")
	}
	if pending, _ := repo.ListPending(ctx); len(pending) != 1 || pending[0].ID() != "dlv-2" {
		t.Errorf("pending after delivery = %v", pending)
	}
	got, err := repo.Get(ctx, "dlv-1")
	if err != nil || got.Status() != domain.DeliveryDelivered || got.Attempts() != 1 {
		t.Errorf("get done: %v %+v", err, got.Snapshot())
	}
	if err := os.WriteFile(filepath.Join(state, "deliveries", "pending", "dlv-3.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ListPending(ctx); err == nil {
		t.Error("a corrupt delivery was listed")
	}
	_ = os.WriteFile(filepath.Join(state, "deliveries", "pending", "dlv-3.json"), []byte(`{"id":"dlv-3"}`), 0o644)
	if _, err := repo.ListPending(ctx); err == nil {
		t.Error("an invalid delivery was listed")
	}
	blocked := NewDeliveryRepository(filepath.Join(state, "deliveries", "pending", "dlv-3.json"))
	if err := blocked.Save(ctx, other); err == nil {
		t.Error("saved under a file")
	}
	bad, _ := domain.NewDelivery("dlv-x", "bot-1", "task-1", "exec-1", "", target, json.RawMessage(`{}`), t0)
	if err := (DeliveryRepository{pending: collection{dir: t.TempDir(), kind: "delivery"}, done: collection{dir: "", kind: "delivery"}}).Save(ctx, bad); err != nil {
		t.Errorf("a missing stale file is not an error: %v", err)
	}
}

func TestArtifactStoreKeepsArtifactsPerExecution(t *testing.T) {
	state := t.TempDir()
	store := NewArtifactStore(state)
	ctx := context.Background()
	if listed, err := store.StoredArtifacts(ctx, "exec-1"); err != nil || len(listed) != 0 {
		t.Fatalf("nothing stored: %v %v", listed, err)
	}
	listing := domain.Artifact{Name: "notes.md", MediaType: "text/markdown", Size: 5, SHA256: strings.Repeat("a", 64), Stored: true}
	if err := store.SaveArtifact(ctx, "exec-1", listing, strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(state, "artifacts", "executions", "exec-1", "notes.md")); err != nil || string(data) != "hello" {
		t.Fatalf("bytes = %q %v", data, err)
	}
	listed, err := store.StoredArtifacts(ctx, "exec-1")
	if err != nil || len(listed) != 1 || listed[0] != listing {
		t.Fatalf("listed = %+v %v", listed, err)
	}
	file, err := store.OpenArtifact(ctx, "exec-1", "notes.md")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(file)
	_ = file.Close()
	if string(data) != "hello" {
		t.Errorf("open = %q", data)
	}
	if _, err := store.OpenArtifact(ctx, "exec-1", "other.md"); !domain.IsNotFound(err) {
		t.Errorf("missing artifact: %v", err)
	}
	// A read that fails keeps nothing, not even a temporary file.
	refused := listing
	refused.Name = "refused.md"
	if err := store.SaveArtifact(ctx, "exec-1", refused, io.MultiReader(strings.NewReader("part"), failingReader{})); err == nil {
		t.Fatal("a failed read was kept")
	}
	entries, _ := os.ReadDir(filepath.Join(state, "artifacts", "executions", "exec-1"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".refused") || e.Name() == "refused.md" {
			t.Errorf("left behind: %s", e.Name())
		}
	}
	for _, bad := range []struct {
		execution domain.ExecutionID
		name      string
	}{{"exec-1", "../escape"}, {"../exec", "notes.md"}, {"", "notes.md"}} {
		if err := store.SaveArtifact(ctx, bad.execution, domain.Artifact{Name: bad.name}, strings.NewReader("x")); err == nil {
			t.Errorf("saved %+v", bad)
		}
		if _, err := store.OpenArtifact(ctx, bad.execution, bad.name); err == nil {
			t.Errorf("opened %+v", bad)
		}
	}
	if _, err := store.StoredArtifacts(ctx, "../exec"); err == nil {
		t.Error("listed outside the store")
	}
	// A listing without its bytes, a stray file and a corrupt listing.
	dir := filepath.Join(state, "artifacts", "executions", "exec-1")
	_ = os.WriteFile(filepath.Join(dir, ".listing", "ghost.md.json"), []byte(`{"name":"ghost.md"}`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, ".listing", "README"), []byte("x"), 0o644)
	if listed, err := store.StoredArtifacts(ctx, "exec-1"); err != nil || len(listed) != 1 {
		t.Errorf("listed = %+v %v", listed, err)
	}
	_ = os.WriteFile(filepath.Join(dir, ".listing", "notes.md.json"), []byte(`{`), 0o644)
	if _, err := store.StoredArtifacts(ctx, "exec-1"); err == nil {
		t.Error("a corrupt listing was read")
	}
	_ = os.WriteFile(filepath.Join(state, "blocker"), []byte("x"), 0o644)
	blocked := NewArtifactStore(filepath.Join(state, "blocker"))
	if err := blocked.SaveArtifact(ctx, "exec-1", listing, strings.NewReader("hello")); err == nil {
		t.Error("saved under a file")
	}
	if _, err := blocked.StoredArtifacts(ctx, "exec-1"); err == nil {
		t.Error("listed under a file")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("the bytes do not match") }
