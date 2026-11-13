package nodeagent

import (
	"context"
	"sync"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// cutoff ends the agent's work the first time the control plane answers
// that the node was revoked: no retry helps then, so the heartbeats,
// polls, renewals and reports stop, and the node says once what to do.
// Before (5 November 2026) a revoked node heartbeat every few seconds
// and retried each report, all refused. The agent then idles until it is
// stopped rather than exit: a pod's sidecar that exits restarts, and a
// crash-looping sidecar takes the pod's service out of rotation.
type cutoff struct {
	node domain.NodeID
	log  application.LogPrinter
	stop context.CancelFunc

	once sync.Once
	done chan struct{}
}

func newCutoff(node domain.NodeID, logger application.LogPrinter, stop context.CancelFunc) *cutoff {
	return &cutoff{node: node, log: logger, stop: stop, done: make(chan struct{})}
}

// revoked reports whether err says the node was revoked, and the first
// time it does, says so and stops the agent's work. A nil cutoff (an
// agent used without Run) only answers.
func (c *cutoff) revoked(err error) bool {
	if !domain.IsRevoked(err) {
		return false
	}
	if c == nil {
		return true
	}
	c.once.Do(func() {
		c.log.Printf("node: %s is revoked: the control plane refuses it for good (%v). The node stops: no heartbeats, polls or reports from here. To bring it back, mint a fresh token on the control plane (axlr-bots node enroll-token --node-id %s), run axlr-node enroll with it on this host, then restart axlr-node (in a pod: put the token in the Secret and delete the pod).", c.node, err, c.node)
		close(c.done)
		c.stop()
	})
	return true
}

// happened reports whether the node was found revoked.
func (c *cutoff) happened() bool {
	if c == nil {
		return false
	}
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}
