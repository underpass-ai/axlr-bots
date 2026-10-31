package application

import (
	"cmp"
	"context"
	"slices"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ListIncidents answers the incidents a query selects, the most recently
// seen first: what an operator scans to see what is on fire.
type ListIncidents struct {
	deps Dependencies
}

// NewListIncidents wires the use case.
func NewListIncidents(deps Dependencies) ListIncidents { return ListIncidents{deps: deps} }

// Execute validates the query and answers at most its limit of
// incidents, the last seen first.
func (uc ListIncidents) Execute(ctx context.Context, query IncidentQuery) ([]domain.Incident, error) {
	var tenant domain.TenantID
	if query.Tenant != "" {
		parsed, err := domain.NewTenantID(query.Tenant)
		if err != nil {
			return nil, err
		}
		tenant = parsed
	}
	var status domain.IncidentStatus
	if query.Status != "" {
		parsed, err := domain.ParseIncidentStatus(query.Status)
		if err != nil {
			return nil, err
		}
		status = parsed
	}
	if query.Limit < 0 {
		return nil, domain.ValidationError{Field: "limit", Reason: "must not be negative"}
	}
	limit := query.Limit
	switch {
	case limit == 0:
		limit = DefaultTaskLimit
	case limit > MaxTaskLimit:
		limit = MaxTaskLimit
	}
	all, err := uc.deps.Incidents.List(ctx)
	if err != nil {
		return nil, err
	}
	incidents := make([]domain.Incident, 0, len(all))
	for _, incident := range all {
		if (tenant == "" || incident.TenantID() == tenant) && (status == "" || incident.Status() == status) {
			incidents = append(incidents, incident)
		}
	}
	slices.SortFunc(incidents, func(a, b domain.Incident) int {
		if c := b.LastSeenAt().Compare(a.LastSeenAt()); c != 0 {
			return c
		}
		return cmp.Compare(a.ID().String(), b.ID().String())
	})
	if len(incidents) > limit {
		incidents = incidents[:limit]
	}
	return incidents, nil
}
