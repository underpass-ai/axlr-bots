// Package nodeagent is the node side of the platform: the process a host
// runs under systemd (`axlr-node`). It registers with the control
// plane, heartbeats, long-polls for the executions of the bots placed on
// the node, runs them with AXLR on the host and reports the result; it
// renews its lease while it runs. Every message goes out from the node;
// the control plane never connects in. The artifacts an execution
// delivered are uploaded from the workspace before the report.
package nodeagent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// Executor runs assignments on the host: the AXLR adapter. It also opens
// what an execution delivered in its workspace.
type Executor interface {
	application.RuntimeExecutor
	application.DecisionExecutor
	application.Workspace
	SupportsPolicy(ctx context.Context) bool
	SupportsCorrelation(ctx context.Context) bool
	Version(ctx context.Context) string
}

// Agent is one node.
type Agent struct {
	Transport application.NodeTransport
	Executor  Executor
	Options   Options

	slots *slots
	wg    *sync.WaitGroup
}

// Run registers and serves until ctx ends. A control plane that is not
// there yet is retried; an execution in flight when ctx ends is
// cancelled and reported as interrupted.
func (a Agent) Run(ctx context.Context) error {
	if a.Transport == nil || a.Executor == nil {
		return errors.New("nodeagent: a transport and an executor are required")
	}
	id, err := domain.NewNodeID(a.Options.NodeID)
	if err != nil {
		return fmt.Errorf("nodeagent: %w", err)
	}
	logger := a.Options.log()
	if err := a.registerUntilUp(ctx, id); err != nil {
		return err
	}
	a.sweepSockets()
	a.slots, a.wg = newSlots(1), &sync.WaitGroup{}
	// Every execution in flight is reported before Run returns, as when
	// the agent ran one at a time.
	defer a.wg.Wait()
	// Heartbeats run on their own, so a long execution does not make the
	// node look dead to the control plane.
	go a.heartbeat(ctx, id)
	for ctx.Err() == nil {
		// One execution works at a time; one that waits for a child it
		// delegated lends its slot, so the child may run here too.
		if !a.slots.acquire(ctx) {
			return nil
		}
		assignment, found, err := a.Transport.NextAssignment(ctx, id, a.Options.pollWait())
		if err != nil {
			a.slots.release()
			if ctx.Err() != nil {
				return nil
			}
			logger.Printf("node: poll: %v", err)
			a.pause(ctx, 2*time.Second)
			continue
		}
		if !found {
			a.slots.release()
			continue
		}
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			defer a.slots.release()
			a.carry(ctx, id, assignment)
		}()
	}
	return nil
}

// sweepSockets removes the delegation sockets a stopped agent left.
func (a Agent) sweepSockets() {
	dir := a.Options.SocketDir
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		a.Options.log().Printf("node: delegation sockets: %v (the delegation tool is off)", err)
		return
	}
	_ = os.Chmod(dir, 0o700)
	stale, _ := filepath.Glob(filepath.Join(dir, "*.sock"))
	for _, path := range stale {
		_ = os.Remove(path)
	}
}

// serveDelegation opens the execution's delegation socket when its bot
// may delegate, and says where it is; "" when the tool is not served.
func (a Agent) serveDelegation(ctx context.Context, id domain.NodeID, execution domain.ExecutionID, offer *application.DelegationOffer) (string, func()) {
	if offer == nil || a.Options.SocketDir == "" || a.slots == nil {
		return "", func() {}
	}
	if strings.ContainsAny(execution.String(), `/\`) || strings.Contains(execution.String(), "..") {
		return "", func() {}
	}
	path := filepath.Join(a.Options.SocketDir, execution.String()+".sock")
	if len(path) >= maxSocketPath {
		a.Options.log().Printf("node: execution %s: socket path %s is too long; the delegation tool is off", execution, path)
		return "", func() {}
	}
	d := delegator{transport: a.Transport, node: id, execution: execution, slots: a.slots, pollWait: a.Options.pollWait(), log: a.Options.log()}
	server, err := serveMCP(ctx, path, delegateTool(*offer), a.Executor.Version(ctx), d.call)
	if err != nil {
		a.Options.log().Printf("node: execution %s: delegation socket: %v; the delegation tool is off", execution, err)
		return "", func() {}
	}
	a.Options.log().Printf("node: execution %s may delegate to %s (depth %d of %d)", execution, strings.Join(offer.Targets, ", "), offer.Depth, offer.MaxDepth)
	return path, server.Close
}

// heartbeat tells the control plane the node is alive until ctx ends; a
// control plane that lost the node (a fresh state) answers not found,
// and the node registers again.
func (a Agent) heartbeat(ctx context.Context, id domain.NodeID) {
	ticker := time.NewTicker(a.Options.heartbeat())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := a.Transport.Heartbeat(ctx, id); err != nil && ctx.Err() == nil {
				a.Options.log().Printf("node: heartbeat: %v", err)
				if domain.IsNotFound(err) {
					_ = a.register(ctx, id)
				}
			}
		}
	}
}

// registerUntilUp registers, retrying while the control plane is down.
func (a Agent) registerUntilUp(ctx context.Context, id domain.NodeID) error {
	for {
		err := a.register(ctx, id)
		if err == nil {
			return nil
		}
		a.Options.log().Printf("node: register: %v (retrying)", err)
		if !a.pause(ctx, 2*time.Second) {
			return nil
		}
	}
}

func (a Agent) register(ctx context.Context, id domain.NodeID) error {
	host := a.Options.HostName
	if host == "" {
		host, _ = os.Hostname()
	}
	registration := application.NodeRegistration{
		ID: id.String(), HostName: host, Labels: a.Options.Labels,
		Capabilities: domain.NodeCapabilities{AXLRVersion: a.Executor.Version(ctx), PolicySupported: a.Executor.SupportsPolicy(ctx), CorrelationSupported: a.Executor.SupportsCorrelation(ctx), Dedicated: a.Options.Dedicated},
	}
	node, err := a.Transport.Register(ctx, registration)
	if err != nil {
		return err
	}
	a.Options.log().Printf("node: registered as %s (%s), axlr %q policy %v correlation %v dedicated %v", node.ID, node.HostName, registration.Capabilities.AXLRVersion, registration.Capabilities.PolicySupported, registration.Capabilities.CorrelationSupported, registration.Capabilities.Dedicated)
	return nil
}

// carry runs one assignment, renewing the lease meanwhile, and reports.
// A lease the control plane refuses (the execution was cancelled, or
// reconciled away) stops the run: its report would be dropped anyway.
func (a Agent) carry(ctx context.Context, id domain.NodeID, assignment application.Assignment) {
	logger := a.Options.log()
	logger.Printf("node: execution %s (%s) assigned, lease until %s", assignment.ExecutionID, assignment.Kind, assignment.LeaseUntil.Format(time.RFC3339))
	runCtx, stopRun := context.WithCancel(ctx)
	defer stopRun()
	go a.renew(runCtx, id, assignment.ExecutionID, stopRun)
	var report application.ExecutionReport
	var err error
	var bot domain.BotID
	switch {
	case assignment.Run != nil:
		bot = assignment.Run.BotID
		request := *assignment.Run
		socket, closeSocket := a.serveDelegation(runCtx, id, assignment.ExecutionID, request.Delegation)
		request.DelegationSocket = socket
		report, err = a.Executor.Execute(runCtx, request)
		closeSocket()
	case assignment.Decide != nil:
		bot = assignment.Decide.BotID
		request := *assignment.Decide
		socket, closeSocket := a.serveDelegation(runCtx, id, assignment.ExecutionID, request.Delegation)
		request.DelegationSocket = socket
		report, err = a.Executor.Decide(runCtx, request)
		closeSocket()
	default:
		err = errors.New("the assignment carries neither a run nor a decision")
	}
	if err != nil {
		stopRun()
		report = application.ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "node " + id.String() + ": " + err.Error(), Policy: report.Policy, Correlation: report.Correlation}
		a.report(id, assignment.ExecutionID, report)
		return
	}
	// The lease is still renewed while the artifacts go up: the
	// control plane takes them only for an execution that runs.
	a.upload(id, assignment.ExecutionID, bot, report.Result.Artifacts)
	stopRun()
	a.report(id, assignment.ExecutionID, report)
}

// upload hands over the bytes of each artifact the execution delivered
// that AXLR lists with a size and a hash (older builds list none). One
// that cannot be read or is refused is logged and left out: the report
// still lists it, and the control plane marks it not kept.
func (a Agent) upload(id domain.NodeID, execution domain.ExecutionID, bot domain.BotID, artifacts []domain.Artifact) {
	logger := a.Options.log()
	for _, artifact := range artifacts {
		if !artifact.Verifiable() {
			continue
		}
		if err := a.uploadOne(id, execution, bot, artifact); err != nil {
			logger.Printf("node: execution %s: artifact %s not uploaded: %v", execution, artifact.Name, err)
			continue
		}
		logger.Printf("node: execution %s: artifact %s uploaded (%d bytes)", execution, artifact.Name, artifact.Size)
	}
}

func (a Agent) uploadOne(id domain.NodeID, execution domain.ExecutionID, bot domain.BotID, artifact domain.Artifact) error {
	file, err := a.Executor.OpenDelivered(bot, artifact.Reference)
	if err != nil {
		return err
	}
	defer file.Close()
	ctx, cancel := context.WithTimeout(context.Background(), a.Options.uploadTimeout())
	defer cancel()
	return a.Transport.UploadArtifact(ctx, id, execution, artifact, file)
}

// renew extends the lease until ctx ends; a lease the control plane
// refuses for good (application.ErrLeaseLost) stops the run.
func (a Agent) renew(ctx context.Context, id domain.NodeID, execution domain.ExecutionID, stopRun context.CancelFunc) {
	ticker := time.NewTicker(a.Options.renew())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := a.Transport.RenewLease(ctx, id, execution); err != nil && ctx.Err() == nil {
				a.Options.log().Printf("node: renew lease of %s: %v", execution, err)
				if errors.Is(err, application.ErrLeaseLost) {
					logger := a.Options.log()
					logger.Printf("node: execution %s: the lease is lost, stopping the run", execution)
					stopRun()
					return
				}
			}
		}
	}
}

// report hands the result over, retrying a few times: the control plane
// accepts a repeated report once, so a retry after a lost answer is safe.
// It uses its own context so a stopping node still reports.
func (a Agent) report(id domain.NodeID, execution domain.ExecutionID, report application.ExecutionReport) {
	logger := a.Options.log()
	for attempt := 1; attempt <= 5; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		ack, err := a.Transport.Report(ctx, id, execution, report)
		cancel()
		if err == nil {
			logger.Printf("node: execution %s reported %s (accepted %v)", execution, report.Outcome, ack.Accepted)
			return
		}
		logger.Printf("node: report %s (attempt %d): %v", execution, attempt, err)
		if domain.IsNotFound(err) {
			return
		}
		time.Sleep(time.Duration(attempt) * time.Second)
	}
}

// pause sleeps unless ctx ends first; false means it ended.
func (a Agent) pause(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
