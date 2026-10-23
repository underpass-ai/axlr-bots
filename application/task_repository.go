package application

import (
	"context"

	"github.com/underpass-ai/underpass-bots/domain"
)

// TaskRepository stores tasks durably and finds them by idempotency key.
type TaskRepository interface {
	Save(ctx context.Context, task domain.Task) error
	// Get returns a domain.NotFoundError when no task has the id.
	Get(ctx context.Context, id domain.TaskID) (domain.Task, error)
	// FindByIdempotencyKey returns the bot's task submitted under the key,
	// and false when none was.
	FindByIdempotencyKey(ctx context.Context, bot domain.BotID, key domain.IdempotencyKey) (domain.Task, bool, error)
}
