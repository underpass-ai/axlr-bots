package axlr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadSessionReadsAXLRsFileWhole(t *testing.T) {
	state := t.TempDir()
	sessions := filepath.Join(state, "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	whole := `{"messages":[` + strings.Repeat(`{"role":"tool","content":"x"},`, 5000) + `{}]}`
	_ = os.WriteFile(filepath.Join(sessions, "abc-1.json"), []byte(whole), 0o600)
	_ = os.Symlink(filepath.Join(sessions, "abc-1.json"), filepath.Join(sessions, "link.json"))
	_ = os.Mkdir(filepath.Join(sessions, "dir.json"), 0o700)
	e := NewExecutor("/nonexistent", t.TempDir())
	e.SessionState = state
	content, err := e.ReadSession("abc-1")
	if err != nil || string(content) != whole {
		t.Fatalf("read: %d bytes %v", len(content), err)
	}
	for _, id := range []string{"", "../x", "a/b", "a.b", strings.Repeat("a", 65), "missing", "link", "dir"} {
		if _, err := e.ReadSession(id); err == nil {
			t.Errorf("%q was read", id)
		}
	}
}

func TestReadSessionResolvesAXLRsStateAsAXLRDoes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".local", "state", "axlr", "sessions")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "s1.json"), []byte("{}"), 0o600)
	e := NewExecutor("/nonexistent", t.TempDir())
	if content, err := e.ReadSession("s1"); err != nil || string(content) != "{}" {
		t.Fatalf("read: %q %v", content, err)
	}
	t.Setenv("HOME", "relative")
	if _, err := e.ReadSession("s1"); err == nil {
		t.Error("read without a state directory")
	}
}
