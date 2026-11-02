package application

import (
	"context"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// NodeOfflineAfter is how long without a heartbeat a node is Offline.
const NodeOfflineAfter = time.Minute

// MarkOfflineNodes marks Offline every Online node not heard from within
// the duration (NodeOfflineAfter when zero) and returns their ids.
type MarkOfflineNodes struct {
	deps Dependencies
	rec  recorder
}

// NewMarkOfflineNodes wires the use case.
func NewMarkOfflineNodes(deps Dependencies) MarkOfflineNodes {
	return MarkOfflineNodes{deps: deps, rec: recorder{deps: deps}}
}

// Execute marks the silent nodes Offline.
func (uc MarkOfflineNodes) Execute(ctx context.Context, after time.Duration) ([]domain.NodeID, error) {
	if after <= 0 {
		after = NodeOfflineAfter
	}
	nodes, err := uc.deps.Nodes.List(ctx)
	if err != nil {
		return nil, err
	}
	now := uc.deps.Clock.Now()
	offline := []domain.NodeID{}
	silent := func(node domain.Node) bool { return node.IsOnline() && !node.SeenWithin(now, after) }
	for _, listed := range nodes {
		if !silent(listed) {
			continue
		}
		// The listing may be stale by now: a heartbeat that landed since
		// keeps the node Online, so the check is made again on the
		// record the update reads.
		var node domain.Node
		marked := false
		err := uc.deps.Nodes.Update(ctx, listed.ID(), func(current *domain.Node) error {
			if !silent(*current) {
				return ErrNodeUnchanged
			}
			current.MarkOffline()
			node, marked = *current, true
			return nil
		})
		if domain.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !marked {
			continue
		}
		if err := uc.rec.nodeEvent(ctx, domain.EventNodeOffline, node.ID(), map[string]any{"lastSeenAt": node.LastSeenAt()}); err != nil {
			return nil, err
		}
		offline = append(offline, node.ID())
	}
	return offline, nil
}
