package filestore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

func TestAutonomyRepositoryRoundTripsAt0600(t *testing.T) {
	dir := t.TempDir()
	repo := NewAutonomyRepository(dir)
	ctx := context.Background()
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	autonomy, err := domain.NewRunbookAutonomy("ops", "fix", "Drift", "shop", domain.AutonomyActWithApproval, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := autonomy.Record(domain.SignalProposalApproved, "inc-1", 1, "tirso", domain.DefaultAutonomyThresholds(), now); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, autonomy); err != nil {
		t.Fatal(err)
	}
	back, err := repo.Get(ctx, autonomy.ID())
	if err != nil || back.Totals().Proposals != 1 || !back.Seen("inc-1", 1, domain.SignalProposalApproved) {
		t.Fatalf("get %+v %v", back.Snapshot(), err)
	}
	info, err := os.Stat(filepath.Join(dir, "autonomy", autonomy.ID().String()+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode %o", mode)
	}
	all, err := repo.List(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("list %v %v", all, err)
	}
	if _, err := repo.Get(ctx, domain.AutonomyID("ladder-missing")); !domain.IsNotFound(err) {
		t.Fatalf("missing: %v", err)
	}
}
