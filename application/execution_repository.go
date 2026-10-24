package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ExecutionRepository stores executions durably.
type ExecutionRepository interface {
	Save(ctx context.Context, execution domain.Execution) error
	// Get returns a domain.NotFoundError when no execution has the id.
	Get(ctx context.Context, id domain.ExecutionID) (domain.Execution, error)
	// ListRunning returns every execution still Running, in id order.
	ListRunning(ctx context.Context) ([]domain.Execution, error)
	// List answers every execution, in no particular order.
	List(ctx context.Context) ([]domain.Execution, error)
}
