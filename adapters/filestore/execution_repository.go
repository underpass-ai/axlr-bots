package filestore

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ExecutionRepository keeps executions under <state>/executions/<id>.json.
type ExecutionRepository struct {
	files collection
	// mu makes Save's check and write one step: copies of the repository
	// share it.
	mu       *sync.Mutex
	outcomes *statusStamps
}

// NewExecutionRepository stores executions under the state directory.
func NewExecutionRepository(stateDir string) ExecutionRepository {
	return ExecutionRepository{files: collection{dir: filepath.Join(stateDir, "executions"), kind: "execution"}, mu: &sync.Mutex{}, outcomes: newStatusStamps()}
}

// Save writes the execution atomically. An execution that has ended
// never runs again, so a Running copy of one that ended is refused (a
// domain.StateError): a lease renewal that read the execution before a
// cancel or a report saved it cannot bring it back.
func (r ExecutionRepository) Save(ctx context.Context, execution domain.Execution) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if execution.Outcome() == domain.ExecutionRunning {
		stored, err := r.Get(ctx, execution.ID())
		switch {
		case err == nil && stored.Outcome().IsFinal():
			return domain.StateError{Subject: "execution " + execution.ID().String(), Current: stored.Outcome().String(), Attempted: "run again"}
		case err != nil && !domain.IsNotFound(err):
			return err
		}
	}
	return r.files.write(execution.ID().String(), execution.Snapshot())
}

// Get reads and validates one execution.
func (r ExecutionRepository) Get(_ context.Context, id domain.ExecutionID) (domain.Execution, error) {
	var snapshot domain.ExecutionSnapshot
	if err := r.files.read(id.String(), &snapshot); err != nil {
		return domain.Execution{}, err
	}
	return domain.RestoreExecution(snapshot)
}

// ListRunning answers the Running executions, in id order. An execution
// whose file has not changed since a listing found it ended is not read
// again.
func (r ExecutionRepository) ListRunning(ctx context.Context) ([]domain.Execution, error) {
	entries, err := r.files.entries()
	if err != nil {
		return nil, err
	}
	running := make([]domain.Execution, 0)
	for _, entry := range entries {
		if r.outcomes.skip(entry, string(domain.ExecutionRunning)) {
			continue
		}
		execution, err := r.Get(ctx, domain.ExecutionID(entry.id))
		if err != nil {
			return nil, err
		}
		r.outcomes.remember(entry, string(execution.Outcome()))
		if execution.Outcome() == domain.ExecutionRunning {
			running = append(running, execution)
		}
	}
	return running, nil
}

// List reads every execution, in the directory's order.
func (r ExecutionRepository) List(ctx context.Context) ([]domain.Execution, error) {
	ids, err := r.files.ids()
	if err != nil {
		return nil, err
	}
	executions := make([]domain.Execution, 0, len(ids))
	for _, id := range ids {
		execution, err := r.Get(ctx, domain.ExecutionID(id))
		if err != nil {
			return nil, err
		}
		executions = append(executions, execution)
	}
	return executions, nil
}
