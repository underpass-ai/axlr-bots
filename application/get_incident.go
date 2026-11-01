package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// GetIncident reads one incident.
type GetIncident struct {
	deps Dependencies
}

// NewGetIncident wires the use case.
func NewGetIncident(deps Dependencies) GetIncident { return GetIncident{deps: deps} }

// Execute returns the incident or a domain.NotFoundError.
func (uc GetIncident) Execute(ctx context.Context, id domain.IncidentID) (domain.Incident, error) {
	return uc.deps.Incidents.Get(ctx, id)
}
