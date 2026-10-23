package system

import (
	"testing"
	"time"

	"github.com/underpass-ai/underpass-bots/domain"
)

func TestClockIsUTCNow(t *testing.T) {
	now := Clock{}.Now()
	if now.Location() != time.UTC || time.Since(now) > time.Minute {
		t.Errorf("Now() = %v", now)
	}
}

func TestIDGeneratorMintsValidUniqueIDs(t *testing.T) {
	gen := IDGenerator{}
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := gen.NewID("exec-")
		if len(id) != len("exec-")+24 || id[:5] != "exec-" {
			t.Fatalf("NewID = %q", id)
		}
		if _, err := domain.NewExecutionID(id); err != nil {
			t.Fatal(err)
		}
		if seen[id] {
			t.Fatalf("duplicate id %s", id)
		}
		seen[id] = true
	}
}
