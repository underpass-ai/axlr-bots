package compose

import (
	"path/filepath"
	"testing"
)

func TestDependencies(t *testing.T) {
	deps, err := Dependencies(t.TempDir(), "axlr-tui", "test")
	if err != nil || deps.Runtime == nil || deps.Artifacts == nil {
		t.Fatalf("wiring: %v", err)
	}
	if _, err := Dependencies("", "axlr-tui", "test"); err == nil {
		t.Error("empty state accepted")
	}
	if _, err := Dependencies(t.TempDir(), "axlr-tui", ""); err == nil {
		t.Error("empty actor accepted")
	}
	blocked := filepath.Join(t.TempDir(), "file")
	if err := writeFile(blocked); err != nil {
		t.Fatal(err)
	}
	if _, err := Dependencies(filepath.Join(blocked, "state"), "axlr-tui", "test"); err == nil {
		t.Error("state under a file accepted")
	}
}
