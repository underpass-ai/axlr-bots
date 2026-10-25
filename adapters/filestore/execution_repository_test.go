package filestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

func TestAnEndedExecutionIsNeverSavedBackAsRunning(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	repo := NewExecutionRepository(dir)
	started := time.Date(2026, 10, 25, 19, 49, 43, 0, time.UTC)
	running, _ := domain.NewExecution("exec-1", "task-1", "bot", started)
	if err := running.AssignTo("web-1"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, running); err != nil {
		t.Fatal(err)
	}
	// A renewal reads it Running; a cancel ends it meanwhile.
	renewal, _ := repo.Get(ctx, "exec-1")
	cancelled, _ := repo.Get(ctx, "exec-1")
	if err := cancelled.Cancel("cancelled by operator", started.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, cancelled); err != nil {
		t.Fatal(err)
	}
	if err := renewal.Lease(started.Add(2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	var state domain.StateError
	if err := repo.Save(ctx, renewal); !errors.As(err, &state) {
		t.Fatalf("the stale Running copy was saved: %v", err)
	}
	if stored, _ := repo.Get(ctx, "exec-1"); stored.Outcome() != domain.ExecutionCancelled {
		t.Errorf("stored = %s", stored.Outcome())
	}
	// A copy of the repository shares the guard; an ended execution may
	// still be saved as ended.
	copied := repo
	if err := copied.Save(ctx, cancelled); err != nil {
		t.Errorf("saving it ended again: %v", err)
	}
	// A record that cannot be read is an error, not a pass.
	if err := os.WriteFile(filepath.Join(dir, "executions", "exec-2.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	broken, _ := domain.NewExecution("exec-2", "task-2", "bot", started)
	if err := repo.Save(ctx, broken); err == nil {
		t.Error("saved over a record it could not read")
	}
}
