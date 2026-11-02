package atomicfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteReplacesTheFileWithTheModeAskedAndLeavesNoTemporary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "record.json")
	if err := Write(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "second" {
		t.Errorf("content = %q %v", data, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v %v", info.Mode().Perm(), err)
	}
	if err := Write(path, []byte("third"), 0o600); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode after a private write = %v", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary left: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("entries = %d", len(entries))
	}
}

func TestWriteFailsWithoutItsDirectory(t *testing.T) {
	if err := Write(filepath.Join(t.TempDir(), "missing", "record.json"), []byte("x"), 0o600); err == nil {
		t.Error("wrote into a directory that does not exist")
	}
}

func TestWriteOverADirectoryFailsAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "taken")
	if err := os.MkdirAll(filepath.Join(target, "child"), 0o750); err != nil {
		t.Fatal(err)
	}
	err := Write(target, []byte("x"), 0o600)
	if err == nil || !strings.Contains(err.Error(), "atomicfile: rename") {
		t.Errorf("err = %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("a temporary was left: %d entries", len(entries))
	}
}

func TestFillFailsOnAClosedFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if err := fill(f, []byte("x"), 0o600); err == nil {
		t.Error("filled a closed file")
	}
}
