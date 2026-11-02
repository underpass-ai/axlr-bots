package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/internal/statelock"
)

// TestASecondDaemonOnTheSameStateIsRefused: before the lock, a second
// serve removed the first one's socket and took it while the first kept
// running its loops over the same files. Now it is refused, the first
// one's socket stays and keeps answering, and once the first stops the
// state is free again.
func TestASecondDaemonOnTheSameStateIsRefused(t *testing.T) {
	f := newFixture(t, "completed")
	client, stop := f.start(t)
	before, err := os.Stat(f.socket)
	if err != nil {
		t.Fatal(err)
	}
	err = Run(context.Background(), f.deps, Options{StateDir: f.state, SocketPath: f.socket})
	if !errors.Is(err, statelock.ErrLocked) {
		t.Fatalf("second daemon = %v", err)
	}
	after, err := os.Stat(f.socket)
	if err != nil || !os.SameFile(before, after) {
		t.Errorf("the first daemon's socket was replaced: %v", err)
	}
	if _, err := client.ListBots(context.Background()); err != nil {
		t.Errorf("the first daemon stopped answering: %v", err)
	}
	// Another socket path does not get around it: the lock is the state's.
	other := filepath.Join(f.dir, "other.sock")
	if err := Run(context.Background(), f.deps, Options{StateDir: f.state, SocketPath: other}); !errors.Is(err, statelock.ErrLocked) {
		t.Errorf("second daemon on another socket = %v", err)
	}
	if unixapi.Reachable(context.Background(), other) {
		t.Error("the second daemon served")
	}
	stop()
	release, err := statelock.Acquire(f.state)
	if err != nil {
		t.Fatalf("the state is still locked after the daemon stopped: %v", err)
	}
	release()
}

func TestDaemonNeedsAStateDirectory(t *testing.T) {
	f := newFixture(t, "completed")
	if err := Run(context.Background(), f.deps, Options{SocketPath: f.socket}); err == nil {
		t.Error("a daemon without a state directory ran")
	}
}
