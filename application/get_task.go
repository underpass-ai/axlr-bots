package application

import (
	"context"

	"github.com/underpass-ai/underpass-bots/domain"
)

// GetTask reads one task.
type GetTask struct {
	deps Dependencies
}

// NewGetTask wires the use case.
func NewGetTask(deps Dependencies) GetTask { return GetTask{deps: deps} }

// Execute returns the task or a domain.NotFoundError.
func (uc GetTask) Execute(ctx context.Context, id domain.TaskID) (domain.Task, error) {
	return uc.deps.Tasks.Get(ctx, id)
}
