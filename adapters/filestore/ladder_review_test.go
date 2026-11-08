package filestore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

var reviewNow = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

func lowered(t *testing.T) domain.RunbookAutonomy {
	t.Helper()
	a, err := domain.NewRunbookAutonomy("ops", "fix", "Drift", "shop", domain.AutonomyActWithApproval, reviewNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Record(domain.SignalVerifyRed, "inc-1", 1, "", domain.DefaultAutonomyThresholds(), reviewNow); err != nil {
		t.Fatal(err)
	}
	if a.Step() != domain.AutonomyObserve {
		t.Fatalf("step %s", a.Step())
	}
	return a
}

// Fail closed at Get: a torn or refused file is an error, not NotFound.
func TestR30_CorruptFileGetFailsClosed(t *testing.T) {
	dir := t.TempDir()
	repo := NewAutonomyRepository(dir)
	a := lowered(t)
	if err := repo.Save(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "autonomy", a.ID().String()+".json")
	if err := os.WriteFile(path, []byte("{\"step\":"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(context.Background(), a.ID()); err == nil || domain.IsNotFound(err) {
		t.Fatalf("get of a torn file: %v", err)
	}
}

// A read-only listing (autonomy list, GET /v1/autonomy) moves the file
// aside: the next load starts a fresh record at the trigger's step, so a
// lowered runbook loses its lowering and its history.
func TestR31_ListQuarantinesAndTheLoweringIsForgotten(t *testing.T) {
	dir := t.TempDir()
	repo := NewAutonomyRepository(dir)
	ctx := context.Background()
	a := lowered(t)
	if err := repo.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "autonomy", a.ID().String()+".json")
	var snapshot map[string]any
	data, _ := os.ReadFile(path)
	_ = json.Unmarshal(data, &snapshot)
	snapshot["history"] = []map[string]any{{"signal": "signal-of-a-newer-binary", "incidentId": "inc-1", "cycle": 1, "at": reviewNow}}
	data, _ = json.Marshal(snapshot)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.List(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, a.ID()); domain.IsNotFound(err) {
		t.Errorf("after a list the record is gone: the keeper now starts at act-with-approval with no history (was %s)", a.Step())
	}
}
