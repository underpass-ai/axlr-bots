package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// GetExecution reads one execution: the result, without the trace.
type GetExecution struct {
	deps Dependencies
}

// NewGetExecution wires the use case.
func NewGetExecution(deps Dependencies) GetExecution { return GetExecution{deps: deps} }

// Execute returns the execution or a domain.NotFoundError.
func (uc GetExecution) Execute(ctx context.Context, id domain.ExecutionID) (domain.Execution, error) {
	return uc.deps.Executions.Get(ctx, id)
}
