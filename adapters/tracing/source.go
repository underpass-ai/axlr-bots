package tracing

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// Source is what the adapter reads to build a trace: three questions,
// not the control plane's whole set of dependencies.
type Source interface {
	// Task answers the task, and false when no task has the id.
	Task(ctx context.Context, id domain.TaskID) (domain.Task, bool, error)
	// Execution answers the execution.
	Execution(ctx context.Context, id domain.ExecutionID) (domain.Execution, error)
	// TaskEvents answers the task's timeline, oldest first.
	TaskEvents(ctx context.Context, id domain.TaskID) ([]domain.Event, error)
}
