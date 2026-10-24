package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// GetNode returns one node.
type GetNode struct {
	deps Dependencies
}

// NewGetNode wires the use case.
func NewGetNode(deps Dependencies) GetNode { return GetNode{deps: deps} }

// Execute reads the node.
func (uc GetNode) Execute(ctx context.Context, id domain.NodeID) (domain.Node, error) {
	return uc.deps.Nodes.Get(ctx, id)
}
