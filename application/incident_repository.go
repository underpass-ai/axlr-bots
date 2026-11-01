package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// IncidentRepository stores incidents durably and finds the newest of a
// group.
type IncidentRepository interface {
	Save(ctx context.Context, incident domain.Incident) error
	// Get returns a domain.NotFoundError when no incident has the id.
	Get(ctx context.Context, id domain.IncidentID) (domain.Incident, error)
	// FindNewestByGroupKey returns the tenant's most recently opened
	// incident of the group key, and false when none was.
	FindNewestByGroupKey(ctx context.Context, tenant domain.TenantID, groupKey string) (domain.Incident, bool, error)
	// List answers every incident, in no particular order.
	List(ctx context.Context) ([]domain.Incident, error)
}
