package filestore

import (
	"context"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/domain"
)

// IncidentRepository keeps incidents under <state>/incidents/<id>.json.
// The lookup by group key scans the directory, as the task lookup by
// idempotency key does.
type IncidentRepository struct {
	files collection
}

// NewIncidentRepository stores incidents under the state directory.
func NewIncidentRepository(stateDir string) IncidentRepository {
	return IncidentRepository{files: collection{dir: filepath.Join(stateDir, "incidents"), kind: "incident"}}
}

// Save writes the incident atomically.
func (r IncidentRepository) Save(_ context.Context, incident domain.Incident) error {
	return r.files.write(incident.ID().String(), incident.Snapshot())
}

// Get reads and validates one incident.
func (r IncidentRepository) Get(_ context.Context, id domain.IncidentID) (domain.Incident, error) {
	var snapshot domain.IncidentSnapshot
	if err := r.files.read(id.String(), &snapshot); err != nil {
		return domain.Incident{}, err
	}
	return domain.RestoreIncident(snapshot)
}

// FindNewestByGroupKey scans the incidents for the tenant's most
// recently opened one of the group key.
func (r IncidentRepository) FindNewestByGroupKey(ctx context.Context, tenant domain.TenantID, groupKey string) (domain.Incident, bool, error) {
	all, err := r.List(ctx)
	if err != nil {
		return domain.Incident{}, false, err
	}
	var newest domain.Incident
	found := false
	for _, incident := range all {
		if incident.TenantID() == tenant && incident.GroupKey() == groupKey && (!found || incident.OpenedAt().After(newest.OpenedAt())) {
			newest, found = incident, true
		}
	}
	return newest, found, nil
}

// List reads every incident, in id order.
func (r IncidentRepository) List(ctx context.Context) ([]domain.Incident, error) {
	ids, err := r.files.ids()
	if err != nil {
		return nil, err
	}
	incidents := make([]domain.Incident, 0, len(ids))
	for _, id := range ids {
		incident, err := r.Get(ctx, domain.IncidentID(id))
		if err != nil {
			return nil, err
		}
		incidents = append(incidents, incident)
	}
	return incidents, nil
}
