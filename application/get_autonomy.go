package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// GetAutonomy reads one runbook's standing.
type GetAutonomy struct {
	deps Dependencies
}

// NewGetAutonomy wires the use case.
func NewGetAutonomy(deps Dependencies) GetAutonomy { return GetAutonomy{deps: deps} }

// Execute answers the record, or a domain.NotFoundError.
func (uc GetAutonomy) Execute(ctx context.Context, id domain.AutonomyID) (AutonomyView, error) {
	autonomy, err := uc.deps.Autonomy.Get(ctx, id)
	if err != nil {
		return AutonomyView{}, err
	}
	return viewOf(ctx, uc.deps, autonomy)
}
