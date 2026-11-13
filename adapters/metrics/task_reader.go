package metrics

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// TaskReader is the one thing the task metrics read of the tasks'
// store: a task by id, for when it was created and what its approval
// wait did.
type TaskReader interface {
	Get(ctx context.Context, id domain.TaskID) (domain.Task, error)
}
