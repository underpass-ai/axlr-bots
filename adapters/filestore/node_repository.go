package filestore

import (
	"context"
	"errors"
	"path/filepath"
	"sync"

	"github.com/underpass-ai/axlr-bots/application"
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

// nodeLocks holds one mutex per node file. It belongs to the process and
// is keyed by the file's path, so every NodeRepository on one state
// directory (the daemon's, a handler's, a test's) takes the same lock;
// another process is kept out by the state directory's lock.
var nodeLocks sync.Map

func (r NodeRepository) lock(id domain.NodeID) func() {
	value, _ := nodeLocks.LoadOrStore(filepath.Join(r.files.dir, id.String()), &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// Save writes the node atomically.
func (r NodeRepository) Save(_ context.Context, node domain.Node) error {
	defer r.lock(node.ID())()
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

// Update reads, changes and writes the node under its lock.
func (r NodeRepository) Update(ctx context.Context, id domain.NodeID, change func(*domain.Node) error) error {
	defer r.lock(id)()
	node, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := change(&node); err != nil {
		if errors.Is(err, application.ErrNodeUnchanged) {
			return nil
		}
		return err
	}
	return r.files.write(id.String(), node.Snapshot())
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
		if r.files.skipListed(id, err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}
