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
	"sync"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/bus/inprocess"
	"github.com/underpass-ai/axlr-bots/adapters/metrics"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/internal/statelock"
)

// Run serves until ctx ends. The socket is removed on the way out.
func Run(ctx context.Context, deps application.Dependencies, options Options) error {
	if options.SocketPath == "" {
		return errors.New("daemon: a socket path is required")
	}
	if options.StateDir == "" {
		return errors.New("daemon: a state directory is required")
	}
	// The lock comes before anything binds: a second daemon on the same
	// state is refused here, before its server would remove the first
	// one's socket and its loops would run the same tasks. It is released
	// last, after the socket's cleanup, so a daemon started next on the
	// same state finds both free.
	release, err := statelock.Acquire(options.StateDir)
	if err != nil {
		return err
	}
	defer release()
	logger := levelLogger{info: options.log(), errors: options.errorLog()}
	domainBus := options.DomainBus
	if domainBus == nil {
		domainBus = inprocess.New()
	}
	deps.DomainEvents = domainBus
	local := unixapi.NewLocal(deps)
	for _, pusher := range options.Pushers {
		local.AddPusher(pusher, logger.Printf)
	}
	// The loops record through the service's dependencies, so a task a
	// timer fires or a reconcile frees wakes the nodes waiting for one.
	deps = local.Dependencies()
	runner := local.Runner()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := domainBus.Listen(ctx, receiver(domainBus, deps, runner)); err != nil {
		return err
	}

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
	metricsListener := options.MetricsListener
	if metricsListener == nil && options.MetricsListenAddr != "" {
		var err error
		if metricsListener, err = net.Listen("tcp", options.MetricsListenAddr); err != nil {
			closeListener(tlsListener)
			closeListener(apiListener)
			return fmt.Errorf("daemon: metrics address: %w", err)
		}
	}
	servers := 1
	served := make(chan error, 4)
	server := unixapi.NewServer(local)
	if metricsListener != nil {
		registry := metrics.NewRegistry()
		registry.MustRegister(metrics.NewOverviewCollector(local))
		server.WithMiddleware(metrics.NewHTTPInstrument(registry).Wrap)
		listening += " and http://" + metricsListener.Addr().String() + "/metrics"
		servers++
		go func() { served <- metrics.Serve(ctx, metricsListener, registry) }()
	}
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

	reconcile(ctx, deps, runner, options, logger)
	// Notifications go out on their own loop: a slow webhook must not
	// hold up the runs, and the runs must not hold up the notifications.
	// The incident outputs have a loop of their own too: a target that
	// does not answer costs its timeout on every attempt, and that must
	// never hold up an approval notification a person is waiting for.
	delivered, announced := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(delivered)
		deliverLoop(ctx, deps, options, logger)
	}()
	go func() {
		defer close(announced)
		outputLoop(ctx, deps, options, logger)
	}()
	defer func() { cancel(); <-delivered; <-announced }()
	// The in-process runs go on the worker's own goroutine: a run lasts
	// up to its task timeout, and the timers and the reconciler must not
	// wait for it. One worker at a time (a tick that finds it busy
	// skips); the way out waits for it to end.
	var working sync.Mutex
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	ticker := time.NewTicker(options.tick())
	defer ticker.Stop()
	reconciler := time.NewTicker(options.reconcileEvery())
	defer reconciler.Stop()
	builds := time.NewTicker(options.buildCheckEvery())
	defer builds.Stop()
	logger.Printf("daemon: serving on %s, tick %s", listening, options.tick())
	logBuild(application.CheckBuild(deps.Build), logger.info)
	for {
		fireTimers(ctx, local, logger)
		if working.TryLock() {
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer working.Unlock()
				drain(ctx, runner, logger)
			}()
		}
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
			reconcile(ctx, deps, runner, options, logger)
		case <-builds.C:
			if status := application.CheckBuild(deps.Build); status.Stale {
				logger.Printf("daemon: the installed binary is %s, this process runs %s: stopping so the service manager starts the installed one", describeBuild(status.Installed), describeBuild(status.Running))
				cancel()
				err := stopped(<-served)
				logger.Printf("daemon: stopped")
				return errors.Join(ErrStaleBuild, err)
			}
		}
	}
}

// receiver is what the domain bus hands another team's event to. The
// bus in process hands it over within the emit, which holds the runner's
// lock already: the bare use case, over the service's dependencies. A
// broker's consumer hands it over from a goroutine of its own: through the
// runner, under its lock.
func receiver(bus DomainBus, deps application.Dependencies, runner *application.SerialRunner) application.DomainEventHandler {
	if _, within := bus.(*inprocess.DomainBus); within {
		receive := application.NewReceiveDomainEvent(deps)
		return func(ctx context.Context, event application.CrossingEvent) error {
			_, err := receive.Execute(ctx, event)
			return err
		}
	}
	return func(ctx context.Context, event application.CrossingEvent) error {
		_, err := runner.ReceiveDomainEvent(ctx, event)
		return err
	}
}

// closeListener closes a listener bound before an option was refused.
func closeListener(listener net.Listener) {
	if listener != nil {
		_ = listener.Close()
	}
}

// fireTimers runs the scheduler's pass: every timer whose slot has come
// fires once (TickTimers), under the same locks as a submission and a
// delete, and the pass is logged.
func fireTimers(ctx context.Context, local *unixapi.Local, logger interface{ Printf(string, ...any) }) {
	if ctx.Err() != nil {
		return
	}
	result, err := local.TickTimers(ctx)
	if err != nil {
		errorf(logger, "daemon: timers: %v", err)
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
		errorf(logger, "daemon: timer %s/%s: %v", failure.BotID, failure.Trigger, failure.Err)
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
		errorf(logger, "daemon: approvals: %v", err)
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
			errorf(logger, "daemon: notifications: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// outputLoop makes the incident output attempts that are due, every
// DeliverEvery, until ctx ends, apart from the notifications.
func outputLoop(ctx context.Context, deps application.Dependencies, options Options, logger interface{ Printf(string, ...any) }) {
	ticker := time.NewTicker(options.deliverEvery())
	defer ticker.Stop()
	uc := application.NewDeliverOutputs(deps)
	for {
		deliverOutputs(ctx, uc, logger)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// deliverOutputs makes one pass of the incident outputs and logs each
// outcome: the target by name, never its URL.
func deliverOutputs(ctx context.Context, uc application.DeliverOutputs, logger interface{ Printf(string, ...any) }) {
	outcomes, err := uc.Execute(ctx)
	for _, outcome := range outcomes {
		d := outcome.Delivery
		line := fmt.Sprintf("daemon: output %s of incident %s (%s) to %s: %s (attempt %d)", d.ID(), d.IncidentID(), d.Event(), d.Output(), d.Status(), d.Attempts())
		if outcome.Reason != "" {
			line += ": " + outcome.Reason
		}
		logger.Printf("%s", line)
	}
	if err != nil && ctx.Err() == nil {
		errorf(logger, "daemon: outputs: %v", err)
	}
}

// drain runs Pending tasks until none is left or ctx ends. Before each
// run it carries out the approval deadlines that passed: an expiry
// takes the runner's slot, as a run does, so it gets its turn between
// two runs rather than after the whole queue. An error ends the drain
// until the next tick: a run that could not start leaves its task
// Pending, the next pick finds it again, and going on at once would
// retry it as fast as the disk answers (QA 5 Nov 2026: a thousand
// refused runs a second, each one an audit line).
func drain(ctx context.Context, runner *application.SerialRunner, logger interface{ Printf(string, ...any) }) {
	for ctx.Err() == nil {
		expireApprovals(ctx, runner, logger)
		result, ran, err := runner.RunNext(ctx)
		if err != nil {
			errorf(logger, "daemon: run: %v", err)
			return
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
// The tasks go through the runner: under its state lock, so a pass
// never fails a task a report or a run is settling, and never a task
// the worker is running in process, whatever the grace.
func reconcile(ctx context.Context, deps application.Dependencies, runner *application.SerialRunner, options Options, logger interface{ Printf(string, ...any) }) {
	if offline, err := application.NewMarkOfflineNodes(deps).Execute(ctx, options.NodeOfflineAfter); err != nil {
		errorf(logger, "daemon: nodes: %v", err)
	} else if len(offline) > 0 {
		logger.Printf("daemon: nodes offline: %v", offline)
	}
	result, err := runner.Reconcile(ctx, options.Grace)
	if err != nil {
		errorf(logger, "daemon: reconcile: %v", err)
		return
	}
	if len(result.Failed) > 0 || len(result.Skipped) > 0 || len(result.Orphaned) > 0 {
		logger.Printf("daemon: reconcile: failed %v, skipped %v, orphaned %v", result.Failed, result.Skipped, result.Orphaned)
	}
}
