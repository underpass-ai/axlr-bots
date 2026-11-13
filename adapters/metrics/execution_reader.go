package metrics

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ExecutionReader is the one thing the task metrics read of the
// executions' store: an execution by id, for its times, its outcome and
// what it consumed.
type ExecutionReader interface {
	Get(ctx context.Context, id domain.ExecutionID) (domain.Execution, error)
}
