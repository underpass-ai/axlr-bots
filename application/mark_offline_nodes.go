package application

import (
	"context"
	"time"

	"github.com/underpass-ai/underpass-bots/domain"
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
	for _, node := range nodes {
		if !node.IsOnline() || node.SeenWithin(now, after) {
			continue
		}
		node.MarkOffline()
		if err := uc.deps.Nodes.Save(ctx, node); err != nil {
			return nil, err
		}
		if err := uc.rec.nodeEvent(ctx, domain.EventNodeOffline, node.ID(), map[string]any{"lastSeenAt": node.LastSeenAt()}); err != nil {
			return nil, err
		}
		offline = append(offline, node.ID())
	}
	return offline, nil
}
