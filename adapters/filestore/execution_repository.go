package filestore

import (
	"context"
	"path/filepath"

	"github.com/underpass-ai/underpass-bots/domain"
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
