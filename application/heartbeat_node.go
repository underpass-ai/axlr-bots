package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// HeartbeatNode marks a node Online as of now.
type HeartbeatNode struct {
	deps Dependencies
	rec  recorder
}

// NewHeartbeatNode wires the use case.
func NewHeartbeatNode(deps Dependencies) HeartbeatNode {
	return HeartbeatNode{deps: deps, rec: recorder{deps: deps}}
}

// Execute records the heartbeat; an unknown node is a NotFoundError, so
// the agent registers again.
func (uc HeartbeatNode) Execute(ctx context.Context, id domain.NodeID) (domain.Node, error) {
	node, err := uc.deps.Nodes.Get(ctx, id)
	if err != nil {
		return domain.Node{}, err
	}
	if err := node.Heartbeat(uc.deps.Clock.Now()); err != nil {
		return domain.Node{}, err
	}
	if err := uc.deps.Nodes.Save(ctx, node); err != nil {
		return domain.Node{}, err
	}
	return node, uc.rec.nodeEvent(ctx, domain.EventNodeHeartbeat, node.ID(), nil)
}
