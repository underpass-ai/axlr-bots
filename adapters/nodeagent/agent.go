// Package nodeagent is the node side of the platform: the process a host
// runs under systemd (`axlr-node`). It registers with the control
// plane, heartbeats, long-polls for the executions of the bots placed on
// the node, runs them with AXLR on the host and reports the result; it
// renews its lease while it runs. Every message goes out from the node;
// the control plane never connects in.
package nodeagent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// Executor runs assignments on the host: the AXLR adapter.
type Executor interface {
	application.RuntimeExecutor
	application.DecisionExecutor
	SupportsPolicy(ctx context.Context) bool
	Version(ctx context.Context) string
}

// Agent is one node.
type Agent struct {
	Transport application.NodeTransport
	Executor  Executor
	Options   Options
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
	// Heartbeats run on their own, so a long execution does not make the
	// node look dead to the control plane.
	go a.heartbeat(ctx, id)
	for ctx.Err() == nil {
		assignment, found, err := a.Transport.NextAssignment(ctx, id, a.Options.pollWait())
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			logger.Printf("node: poll: %v", err)
			a.pause(ctx, 2*time.Second)
			continue
		}
		if !found {
			continue
		}
		a.carry(ctx, id, assignment)
	}
	return nil
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
		Capabilities: domain.NodeCapabilities{AXLRVersion: a.Executor.Version(ctx), PolicySupported: a.Executor.SupportsPolicy(ctx)},
	}
	node, err := a.Transport.Register(ctx, registration)
	if err != nil {
		return err
	}
	a.Options.log().Printf("node: registered as %s (%s), axlr %q policy %v", node.ID, node.HostName, registration.Capabilities.AXLRVersion, registration.Capabilities.PolicySupported)
	return nil
}

// carry runs one assignment, renewing the lease meanwhile, and reports.
func (a Agent) carry(ctx context.Context, id domain.NodeID, assignment application.Assignment) {
	logger := a.Options.log()
	logger.Printf("node: execution %s (%s) assigned, lease until %s", assignment.ExecutionID, assignment.Kind, assignment.LeaseUntil.Format(time.RFC3339))
	runCtx, stopRenewing := context.WithCancel(ctx)
	go a.renew(runCtx, id, assignment.ExecutionID)
	var report application.ExecutionReport
	var err error
	switch {
	case assignment.Run != nil:
		report, err = a.Executor.Execute(ctx, *assignment.Run)
	case assignment.Decide != nil:
		report, err = a.Executor.Decide(ctx, *assignment.Decide)
	default:
		err = errors.New("the assignment carries neither a run nor a decision")
	}
	stopRenewing()
	if err != nil {
		report = application.ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "node " + id.String() + ": " + err.Error(), Policy: report.Policy}
	}
	a.report(id, assignment.ExecutionID, report)
}

// renew extends the lease until ctx ends.
func (a Agent) renew(ctx context.Context, id domain.NodeID, execution domain.ExecutionID) {
	ticker := time.NewTicker(a.Options.renew())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := a.Transport.RenewLease(ctx, id, execution); err != nil && ctx.Err() == nil {
				a.Options.log().Printf("node: renew lease of %s: %v", execution, err)
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
