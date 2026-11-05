package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// stopOnSignal is a context that ends on SIGTERM or SIGINT, its cause
// naming the signal: a run it stops (a pod deleted, a unit stopped)
// reports "cancelled: the node is stopping (terminated)", not "context
// canceled". The returned stop releases the signals.
func stopOnSignal(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		select {
		case s := <-signals:
			cancel(fmt.Errorf("the node is stopping (%s)", s))
		case <-ctx.Done():
		}
	}()
	return ctx, func() {
		signal.Stop(signals)
		cancel(context.Canceled)
	}
}
