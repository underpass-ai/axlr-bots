// Package daemon is the control plane as a long-running service: it
// serves the local API on a Unix socket, runs Pending tasks one at a
// time and reconciles the tasks a dead run left Running. The state
// directory is the spool: a task file that appears there, through the
// socket or written by a command while the daemon was down, is run on
// the next tick, and a restart of the daemon loses nothing that was
// saved.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/underpass-ai/underpass-bots/adapters/unixapi"
	"github.com/underpass-ai/underpass-bots/application"
)

// Run serves until ctx ends. The socket is removed on the way out.
func Run(ctx context.Context, deps application.Dependencies, options Options) error {
	if options.SocketPath == "" {
		return errors.New("daemon: a socket path is required")
	}
	logger := options.log()
	local := unixapi.NewLocal(deps)
	runner := local.Runner()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	served := make(chan error, 1)
	go func() { served <- unixapi.NewServer(local).Serve(ctx, options.SocketPath) }()

	reconcile(ctx, deps, options, logger)
	ticker := time.NewTicker(options.tick())
	defer ticker.Stop()
	reconciler := time.NewTicker(options.reconcileEvery())
	defer reconciler.Stop()
	logger.Printf("daemon: serving on %s, tick %s", options.SocketPath, options.tick())
	for {
		drain(ctx, runner, logger)
		select {
		case <-ctx.Done():
			err := <-served
			logger.Printf("daemon: stopped")
			return err
		case err := <-served:
			if err == nil {
				err = errors.New("daemon: the API server stopped")
			}
			return err
		case <-ticker.C:
		case <-reconciler.C:
			reconcile(ctx, deps, options, logger)
		}
	}
}

// drain runs Pending tasks until none is left or ctx ends.
func drain(ctx context.Context, runner *application.SerialRunner, logger interface{ Printf(string, ...any) }) {
	for ctx.Err() == nil {
		result, ran, err := runner.RunNext(ctx)
		if err != nil {
			logger.Printf("daemon: run: %v", err)
			if !ran {
				return
			}
			continue
		}
		if !ran {
			return
		}
		line := fmt.Sprintf("daemon: task %s %s (execution %s", result.Task.ID(), result.Task.Status(), result.Execution.ID())
		if reason := result.Execution.FailureReason(); reason != "" {
			line += ", " + reason
		}
		if report := application.ReportReference(result.Execution); report != "" {
			line += ", report " + report
		}
		logger.Printf("%s)", line)
	}
}

// reconcile fails the tasks a dead run left Running past the grace.
func reconcile(ctx context.Context, deps application.Dependencies, options Options, logger interface{ Printf(string, ...any) }) {
	result, err := application.NewReconcileTasks(deps).Execute(ctx, options.Grace)
	if err != nil {
		logger.Printf("daemon: reconcile: %v", err)
		return
	}
	if len(result.Failed) > 0 || len(result.Skipped) > 0 {
		logger.Printf("daemon: reconcile: failed %v, skipped %v", result.Failed, result.Skipped)
	}
}
