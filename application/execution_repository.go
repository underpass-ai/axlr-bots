package application

import (
	"context"

	"github.com/underpass-ai/underpass-bots/domain"
)

// ExecutionRepository stores executions durably.
type ExecutionRepository interface {
	Save(ctx context.Context, execution domain.Execution) error
	// Get returns a domain.NotFoundError when no execution has the id.
	Get(ctx context.Context, id domain.ExecutionID) (domain.Execution, error)
}
