package application

import (
	"context"

	"github.com/underpass-ai/underpass-bots/domain"
)

// ListNodes returns every node.
type ListNodes struct {
	deps Dependencies
}

// NewListNodes wires the use case.
func NewListNodes(deps Dependencies) ListNodes { return ListNodes{deps: deps} }

// Execute lists the nodes.
func (uc ListNodes) Execute(ctx context.Context) ([]domain.Node, error) {
	return uc.deps.Nodes.List(ctx)
}
