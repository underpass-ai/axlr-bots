package tracing

import (
	"context"
	"errors"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// repositorySource answers Source from the application's ports.
type repositorySource struct {
	tasks      application.TaskRepository
	executions application.ExecutionRepository
	events     application.EventReader
}

// NewSource answers Source from the three ports it needs.
func NewSource(tasks application.TaskRepository, executions application.ExecutionRepository, events application.EventReader) Source {
	return repositorySource{tasks: tasks, executions: executions, events: events}
}

func (s repositorySource) Task(ctx context.Context, id domain.TaskID) (domain.Task, bool, error) {
	task, err := s.tasks.Get(ctx, id)
	var missing domain.NotFoundError
	if errors.As(err, &missing) {
		return domain.Task{}, false, nil
	}
	if err != nil {
		return domain.Task{}, false, err
	}
	return task, true, nil
}

func (s repositorySource) Execution(ctx context.Context, id domain.ExecutionID) (domain.Execution, error) {
	return s.executions.Get(ctx, id)
}

func (s repositorySource) TaskEvents(ctx context.Context, id domain.TaskID) ([]domain.Event, error) {
	return s.events.ListTaskEvents(ctx, id, application.MaxEventLimit)
}
