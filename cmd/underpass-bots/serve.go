package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os/signal"
	"syscall"

	"github.com/underpass-ai/underpass-bots/adapters/daemon"
	"github.com/underpass-ai/underpass-bots/application"
)

// serve runs the control plane as a service until SIGTERM or SIGINT.
func serve(ctx context.Context, app application.Dependencies, socketPath string, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	tick := flags.Duration("tick", daemon.DefaultTick, "how often to look for Pending tasks")
	reconcileEvery := flags.Duration("reconcile-every", daemon.DefaultReconcileEvery, "how often to fail the tasks a dead run left Running")
	grace := flags.Duration("grace", 0, "reconciliation grace (default: the bot's task timeout plus 1m)")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "underpass-bots: serve takes no arguments")
		return exitUsage
	}
	if socketPath == "" {
		fmt.Fprintln(stderr, "underpass-bots: serve needs a socket (--socket or UNDERPASS_SOCKET)")
		return exitUsage
	}
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	logger := log.New(stderr, "", log.LstdFlags)
	if err := daemon.Run(ctx, app, daemon.Options{SocketPath: socketPath, Tick: *tick, ReconcileEvery: *reconcileEvery, Grace: *grace, Log: logger}); err != nil {
		fmt.Fprintln(stderr, "underpass-bots:", err)
		return exitError
	}
	return exitOK
}
