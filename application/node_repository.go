package application

import (
	"context"
	"errors"

	"github.com/underpass-ai/axlr-bots/domain"
)

// NodeRepository stores nodes durably.
type NodeRepository interface {
	Save(ctx context.Context, node domain.Node) error
	// Get returns a domain.NotFoundError when no node has the id.
	Get(ctx context.Context, id domain.NodeID) (domain.Node, error)
	List(ctx context.Context) ([]domain.Node, error)
	// Update reads the node, applies change and saves the result, with
	// no other Update or Save of that node in between: a heartbeat read
	// before a revoke cannot be saved after it and bring the node back.
	// An unknown node is a domain.NotFoundError and change is not
	// called; a change that returns ErrNodeUnchanged writes nothing and
	// Update returns nil; any other error writes nothing and is returned.
	Update(ctx context.Context, id domain.NodeID, change func(*domain.Node) error) error
}

// ErrNodeUnchanged is what a NodeRepository.Update change returns when
// the node needs no write.
var ErrNodeUnchanged = errors.New("node unchanged")
