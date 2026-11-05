package main

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestStopOnSignalNamesTheSignal(t *testing.T) {
	ctx, stop := stopOnSignal(context.Background())
	defer stop()
	_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the signal did not end the context")
	}
	if got := context.Cause(ctx).Error(); got != "the node is stopping (interrupt)" {
		t.Errorf("cause = %q", got)
	}
}

func TestStopOnSignalStop(t *testing.T) {
	ctx, stop := stopOnSignal(context.Background())
	stop()
	<-ctx.Done()
	if context.Cause(ctx) != context.Canceled {
		t.Errorf("cause = %v", context.Cause(ctx))
	}
}
