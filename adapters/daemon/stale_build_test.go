package daemon

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// swappableBuilds is a binary that a test replaces on disk.
type swappableBuilds struct {
	running   domain.BuildIdentity
	installed chan domain.BuildIdentity
	current   domain.BuildIdentity
}

func (s *swappableBuilds) Running() domain.BuildIdentity { return s.running }
func (s *swappableBuilds) Installed() (domain.BuildIdentity, error) {
	select {
	case next := <-s.installed:
		s.current = next
	default:
	}
	return s.current, nil
}

func TestDaemonStopsWhenANewerBinaryIsInstalled(t *testing.T) {
	f := newFixture(t, "completed")
	old := domain.BuildIdentity{Revision: "1111111111111111", Time: "2026-11-12T08:00:00Z"}
	builds := &swappableBuilds{running: old, current: old, installed: make(chan domain.BuildIdentity, 1)}
	f.deps.Build = builds
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	options := Options{StateDir: f.state, SocketPath: f.socket, Tick: 20 * time.Millisecond, BuildCheckEvery: 20 * time.Millisecond, Log: log.New(f.logs, "", 0)}
	go func() { done <- Run(ctx, f.deps, options) }()
	deadline := time.Now().Add(5 * time.Second)
	for !unixapi.Reachable(context.Background(), f.socket) {
		if time.Now().After(deadline) {
			t.Fatal("daemon not reachable")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The same build on disk keeps it serving through several checks.
	time.Sleep(100 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("stopped with the same build installed: %v", err)
	default:
	}
	builds.installed <- domain.BuildIdentity{Revision: "2222222222222222", Modified: true}
	select {
	case err := <-done:
		if !errors.Is(err, ErrStaleBuild) {
			t.Fatalf("got %v, want ErrStaleBuild", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the daemon did not stop")
	}
	logs := f.logs.String()
	for _, want := range []string{"daemon: build 111111111111 (2026-11-12T08:00:00Z)", "the installed binary is 222222222222 with changes, this process runs 111111111111 (2026-11-12T08:00:00Z)", "daemon: stopped"} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs lack %q:\n%s", want, logs)
		}
	}
}

func TestLogBuildSaysWhatItCannotCompare(t *testing.T) {
	var out bytes.Buffer
	logBuild(application.BuildStatus{Reason: "no build reader"}, log.New(&out, "", 0))
	if got := out.String(); got != "daemon: build without a commit (installed build not compared: no build reader)\n" {
		t.Fatalf("got %q", got)
	}
}
