package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// channelNodeOf is the node that calls a team channel route (ask a team,
// read a question, watch the team's questions), refused unless it runs
// the team channel (axlr-node --team-channel, off by default). The
// control plane never offers such a node the tools, so a call from it is
// a node that should not make one: an error, never a refusal the model
// reads.
func channelNodeOf(ctx context.Context, deps Dependencies, id domain.NodeID, attempted string) (domain.Node, error) {
	node, err := deps.Nodes.Get(ctx, id)
	if err != nil {
		return domain.Node{}, err
	}
	if !node.Capabilities().TeamChannel {
		return domain.Node{}, domain.ForbiddenError{Reason: "node " + id.String() + " runs with the team channel off (axlr-node --team-channel): it may not " + attempted}
	}
	return node, nil
}
