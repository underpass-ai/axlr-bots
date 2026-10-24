package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// NodeRepository stores nodes durably.
type NodeRepository interface {
	Save(ctx context.Context, node domain.Node) error
	// Get returns a domain.NotFoundError when no node has the id.
	Get(ctx context.Context, id domain.NodeID) (domain.Node, error)
	List(ctx context.Context) ([]domain.Node, error)
}
