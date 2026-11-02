package statelock

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquireRefusesASecondHolderUntilTheFirstReleases(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	release, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("lock file = %v %v", info, err)
	}
	_, err = Acquire(dir)
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("second acquire = %v", err)
	}
	var locked LockedError
	if !errors.As(err, &locked) || locked.Dir != dir || locked.Path != filepath.Join(dir, FileName) {
		t.Errorf("locked error = %+v", locked)
	}
	want := "the state at " + dir + " is in use by another process (a daemon holds " + filepath.Join(dir, FileName) + "); use the socket or stop it"
	if err.Error() != want {
		t.Errorf("message = %q", err.Error())
	}
	release()
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Errorf("the lock file was removed: %v", err)
	}
	again, err := Acquire(dir)
	if err != nil {
		t.Fatalf("acquire after the release = %v", err)
	}
	again()
}

func TestAcquireFailsWhereNoLockFileCanBe(t *testing.T) {
	state := t.TempDir()
	blocker := filepath.Join(state, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(blocker); err == nil || errors.Is(err, ErrLocked) {
		t.Errorf("a file as the state directory = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(state, "dir", FileName), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(filepath.Join(state, "dir")); err == nil || !strings.HasPrefix(err.Error(), "statelock:") {
		t.Errorf("a directory as the lock file = %v", err)
	}
}
