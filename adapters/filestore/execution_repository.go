package filestore

import (
	"context"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ExecutionRepository keeps executions under <state>/executions/<id>.json.
type ExecutionRepository struct {
	files collection
}

// NewExecutionRepository stores executions under the state directory.
func NewExecutionRepository(stateDir string) ExecutionRepository {
	return ExecutionRepository{files: collection{dir: filepath.Join(stateDir, "executions"), kind: "execution"}}
}

// Save writes the execution atomically.
func (r ExecutionRepository) Save(_ context.Context, execution domain.Execution) error {
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

// ListRunning scans every execution and keeps the Running ones, in id order.
func (r ExecutionRepository) ListRunning(ctx context.Context) ([]domain.Execution, error) {
	ids, err := r.files.ids()
	if err != nil {
		return nil, err
	}
	running := make([]domain.Execution, 0)
	for _, id := range ids {
		execution, err := r.Get(ctx, domain.ExecutionID(id))
		if err != nil {
			return nil, err
		}
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
