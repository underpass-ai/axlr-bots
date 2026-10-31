package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// NodePusher is the bus port on the control plane's side: it carries a
// push to one node, which hears it on the adapter's other end
// (nodeagent.Streamer). A push is a hint about an execution the node
// runs; the control plane's record stays the source of truth, and a node
// that missed pushes asks for everything on its next resync. An adapter
// never blocks the caller for long: the use cases push while they hold
// the runner's lock, so one that speaks to a broker queues the push and
// sends it from its own goroutine (an outbox).
type NodePusher interface {
	Push(ctx context.Context, node domain.NodeID, push NodePush) error
}
