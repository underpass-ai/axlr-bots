package application

import (
	"context"

	"github.com/underpass-ai/underpass-bots/domain"
)

// GetReport reads a task's report: the answer of its completed execution,
// as the artifact store kept it.
type GetReport struct {
	deps Dependencies
}

// NewGetReport wires the use case.
func NewGetReport(deps Dependencies) GetReport {
	return GetReport{deps: deps}
}

// Execute returns the report; a domain.NotFoundError names a task that
// does not exist or has no report yet.
func (uc GetReport) Execute(ctx context.Context, id domain.TaskID) ([]byte, error) {
	if _, err := uc.deps.Tasks.Get(ctx, id); err != nil {
		return nil, err
	}
	return uc.deps.Artifacts.LoadReport(ctx, id)
}
