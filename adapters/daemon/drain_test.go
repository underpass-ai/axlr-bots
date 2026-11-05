package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
)

// countingLog counts the lines the drain writes.
type countingLog struct{ lines []string }

func (l *countingLog) Printf(format string, args ...any) {
	l.lines = append(l.lines, format)
}

// A run that cannot start leaves its task Pending; the drain logs it
// once and waits for the next tick instead of picking it again at once.
func TestDrainStopsAtARunThatCannotStart(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes a read-only directory")
	}
	f := newFixture(t, "completed")
	ctx := context.Background()
	if _, err := application.NewRegisterBot(f.deps).Execute(ctx, application.BotDefinition{Tenant: "ops", Name: "b", Owner: "o", Mission: "m"}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.NewEnableBot(f.deps).Execute(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	if _, err := application.NewSubmitTask(f.deps).Execute(ctx, application.SubmitTaskCommand{BotID: "b", Objective: "o", IdempotencyKey: "k"}); err != nil {
		t.Fatal(err)
	}
	// The execution cannot be saved: the run is refused before it starts.
	executions := filepath.Join(f.state, "executions")
	if err := os.MkdirAll(executions, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(executions, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(executions, 0o700) })
	logger := &countingLog{}
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		drain(runCtx, application.NewSerialRunner(f.deps), logger)
		close(done)
	}()
	select {
	case <-done:
	case <-runCtx.Done():
		<-done
		t.Fatalf("the drain kept retrying until its context ended: %d lines", len(logger.lines))
	}
	if len(logger.lines) != 1 || !strings.HasPrefix(logger.lines[0], "daemon: run:") {
		t.Errorf("lines = %q", logger.lines)
	}
}
