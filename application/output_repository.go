package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// OutputRepository keeps the control plane's output targets, URLs and
// secrets included, where only the daemon's user reads them.
type OutputRepository interface {
	// List returns every target, in name order.
	List(ctx context.Context) ([]domain.OutputTarget, error)
	// Get returns a domain.NotFoundError when no target has the name.
	Get(ctx context.Context, name domain.OutputName) (domain.OutputTarget, error)
	// Add keeps a new target; a domain.ConflictError when the name is
	// taken.
	Add(ctx context.Context, target domain.OutputTarget) error
	// Remove drops the target; a domain.NotFoundError when there is none.
	Remove(ctx context.Context, name domain.OutputName) error
}
