package filestore

import (
	"context"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/domain"
)

// NodeRepository keeps nodes under <state>/nodes/<id>.json.
type NodeRepository struct {
	files collection
}

// NewNodeRepository stores nodes under the state directory.
func NewNodeRepository(stateDir string) NodeRepository {
	return NodeRepository{files: collection{dir: filepath.Join(stateDir, "nodes"), kind: "node"}}
}

// Save writes the node atomically.
func (r NodeRepository) Save(_ context.Context, node domain.Node) error {
	return r.files.write(node.ID().String(), node.Snapshot())
}

// Get reads and validates one node.
func (r NodeRepository) Get(_ context.Context, id domain.NodeID) (domain.Node, error) {
	var snapshot domain.NodeSnapshot
	if err := r.files.read(id.String(), &snapshot); err != nil {
		return domain.Node{}, err
	}
	return domain.RestoreNode(snapshot)
}

// List reads every node, in id order.
func (r NodeRepository) List(ctx context.Context) ([]domain.Node, error) {
	ids, err := r.files.ids()
	if err != nil {
		return nil, err
	}
	nodes := make([]domain.Node, 0, len(ids))
	for _, id := range ids {
		node, err := r.Get(ctx, domain.NodeID(id))
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}
