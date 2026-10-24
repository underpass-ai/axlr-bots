package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ListExecutions reads a task's executions, oldest first.
type ListExecutions struct {
	deps Dependencies
}

// NewListExecutions wires the use case.
func NewListExecutions(deps Dependencies) ListExecutions {
	return ListExecutions{deps: deps}
}

// Execute returns the executions of the task.
func (uc ListExecutions) Execute(ctx context.Context, id domain.TaskID) ([]domain.Execution, error) {
	task, err := uc.deps.Tasks.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	ids := task.ExecutionIDs()
	executions := make([]domain.Execution, 0, len(ids))
	for _, executionID := range ids {
		execution, err := uc.deps.Executions.Get(ctx, executionID)
		if err != nil {
			return nil, err
		}
		executions = append(executions, execution)
	}
	return executions, nil
}
