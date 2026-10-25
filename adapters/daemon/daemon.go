// Package daemon is the control plane as a long-running service: it
// serves the API on a Unix socket (the node protocol on a TLS address
// for nodes on other hosts, and the operator routes on another TLS
// address behind a bearer token), hands executions to the nodes that
// poll for them, runs in process the Pending tasks no Online node may
// run (the single-process mode), fires the timer triggers whose slot
// has come, carries out the deadline of every pending call nobody
// decided in time, delivers the notifications of approval requests,
// marks silent nodes Offline and reconciles the tasks a dead run left
// Running. The state directory is the spool: a task file that
// appears there, through the socket or written by a command while the
// daemon was down, is picked up on the next tick, a timer whose slot
// passed while the daemon was down fires once, and a restart of the
// daemon loses nothing that was saved: an approval that expired or a
// notification that came due while it was down is handled at the start.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
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

	// The TLS listeners are bound before anything serves, so a bad option
	// returns before the socket exists and no server is left behind.
	var tlsListener, apiListener net.Listener
	listening := options.SocketPath
	if options.ListenAddr != "" || options.Listener != nil {
		if options.TLS == nil {
			return errors.New("daemon: the TLS address needs a TLS configuration (the fleet CA)")
		}
		tlsListener = options.Listener
		if tlsListener == nil {
			var err error
			if tlsListener, err = unixapi.Listen(options.ListenAddr); err != nil {
				return err
			}
		}
		listening += " and https://" + tlsListener.Addr().String() + " (nodes)"
	}
	if options.APIListenAddr != "" || options.APIListener != nil {
		if options.TLS == nil {
			closeListener(tlsListener)
			return errors.New("daemon: the operator address needs a TLS configuration (the fleet CA)")
		}
		if options.APIToken == "" {
			closeListener(tlsListener)
			return errors.New("daemon: the operator address needs a bearer token")
		}
		apiListener = options.APIListener
		if apiListener == nil {
			var err error
			if apiListener, err = unixapi.Listen(options.APIListenAddr); err != nil {
				closeListener(tlsListener)
				return err
			}
		}
		listening += " and https://" + apiListener.Addr().String() + " (operators, bearer token)"
	}
	servers := 1
	served := make(chan error, 3)
	server := unixapi.NewServer(local)
	go func() { served <- server.Serve(ctx, options.SocketPath) }()
	if tlsListener != nil {
		servers++
		go func() { served <- server.ServeTLS(ctx, tlsListener, options.TLS) }()
	}
	if apiListener != nil {
		servers++
		go func() { served <- server.ServeAPI(ctx, apiListener, options.TLS, options.APIToken) }()
	}
	// stopped waits for every server to end; the socket's cleanup must
	// have run before Run returns, or a daemon started next on the same
	// path would lose its socket to it.
	stopped := func(first error) error {
		for range servers - 1 {
			if err := <-served; first == nil {
				first = err
			}
		}
		return first
	}

	reconcile(ctx, deps, options, logger)
	// Notifications go out on their own loop: a slow webhook must not
	// hold up the runs, and the runs must not hold up the notifications.
	delivered := make(chan struct{})
	go func() {
		defer close(delivered)
		deliverLoop(ctx, deps, options, logger)
	}()
	defer func() { cancel(); <-delivered }()
	ticker := time.NewTicker(options.tick())
	defer ticker.Stop()
	reconciler := time.NewTicker(options.reconcileEvery())
	defer reconciler.Stop()
	logger.Printf("daemon: serving on %s, tick %s", listening, options.tick())
	for {
		fireTimers(ctx, deps, logger)
		expireApprovals(ctx, runner, logger)
		drain(ctx, runner, logger)
		select {
		case <-ctx.Done():
			err := stopped(<-served)
			logger.Printf("daemon: stopped")
			return err
		case err := <-served:
			if ctx.Err() != nil {
				// The stop was asked for; the server ended because of it.
				err = stopped(err)
				logger.Printf("daemon: stopped")
				return err
			}
			if err == nil {
				err = errors.New("daemon: the API server stopped")
			}
			cancel()
			return stopped(err)
		case <-ticker.C:
		case <-reconciler.C:
			reconcile(ctx, deps, options, logger)
		}
	}
}

// closeListener closes a listener bound before an option was refused.
func closeListener(listener net.Listener) {
	if listener != nil {
		_ = listener.Close()
	}
}

// fireTimers runs the scheduler's pass: every timer whose slot has come
// fires once (TickTimers), and the pass is logged.
func fireTimers(ctx context.Context, deps application.Dependencies, logger interface{ Printf(string, ...any) }) {
	if ctx.Err() != nil {
		return
	}
	result, err := application.NewTickTimers(deps).Execute(ctx)
	if err != nil {
		logger.Printf("daemon: timers: %v", err)
		return
	}
	for _, fire := range result.Fired {
		verb := "created"
		if !fire.Created {
			verb = "found"
		}
		logger.Printf("daemon: timer %s/%s fired for %s, %s task %s", fire.BotID, fire.Trigger, fire.ScheduledAt.UTC().Format(time.RFC3339), verb, fire.TaskID)
	}
	for _, failure := range result.Errors {
		logger.Printf("daemon: timer %s/%s: %v", failure.BotID, failure.Trigger, failure.Err)
	}
}

// expireApprovals carries out every deadline that passed (deny or
// cancel, as each bot says) and logs what it did.
func expireApprovals(ctx context.Context, runner *application.SerialRunner, logger interface{ Printf(string, ...any) }) {
	if ctx.Err() != nil {
		return
	}
	done, err := runner.ExpireApprovals(ctx)
	for _, expired := range done {
		line := fmt.Sprintf("daemon: approval of task %s expired: %s", expired.TaskID, expired.Action)
		if expired.Execution != nil {
			line += fmt.Sprintf(" (execution %s, task %s)", expired.Execution.ID(), expired.Task.Status())
		}
		logger.Printf("%s", line)
	}
	if err != nil {
		logger.Printf("daemon: approvals: %v", err)
	}
}

// deliverLoop makes the notification attempts that are due, every
// DeliverEvery, until ctx ends.
func deliverLoop(ctx context.Context, deps application.Dependencies, options Options, logger interface{ Printf(string, ...any) }) {
	ticker := time.NewTicker(options.deliverEvery())
	defer ticker.Stop()
	uc := application.NewDeliverNotifications(deps)
	for {
		outcomes, err := uc.Execute(ctx)
		for _, outcome := range outcomes {
			d := outcome.Delivery
			line := fmt.Sprintf("daemon: notification %s of task %s to %s: %s (attempt %d)", d.ID(), d.TaskID(), d.Target(), d.Status(), d.Attempts())
			if outcome.Reason != "" {
				line += ": " + outcome.Reason
			}
			logger.Printf("%s", line)
		}
		if err != nil && ctx.Err() == nil {
			logger.Printf("daemon: notifications: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
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

// reconcile marks silent nodes Offline and fails the tasks a dead run
// left Running past the grace or whose node stopped renewing its lease.
func reconcile(ctx context.Context, deps application.Dependencies, options Options, logger interface{ Printf(string, ...any) }) {
	if offline, err := application.NewMarkOfflineNodes(deps).Execute(ctx, options.NodeOfflineAfter); err != nil {
		logger.Printf("daemon: nodes: %v", err)
	} else if len(offline) > 0 {
		logger.Printf("daemon: nodes offline: %v", offline)
	}
	result, err := application.NewReconcileTasks(deps).Execute(ctx, options.Grace)
	if err != nil {
		logger.Printf("daemon: reconcile: %v", err)
		return
	}
	if len(result.Failed) > 0 || len(result.Skipped) > 0 {
		logger.Printf("daemon: reconcile: failed %v, skipped %v", result.Failed, result.Skipped)
	}
}
