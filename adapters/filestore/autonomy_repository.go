package filestore

import (
	"context"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/domain"
)

// AutonomyRepository keeps each runbook's standing under
// <state>/autonomy/<id>.json.
type AutonomyRepository struct {
	files collection
}

// NewAutonomyRepository stores the records under the state directory.
func NewAutonomyRepository(stateDir string) AutonomyRepository {
	return AutonomyRepository{files: collection{dir: filepath.Join(stateDir, "autonomy"), kind: "autonomy"}}
}

// Save writes the record atomically.
func (r AutonomyRepository) Save(_ context.Context, autonomy domain.RunbookAutonomy) error {
	return r.files.write(autonomy.ID().String(), autonomy.Snapshot())
}

// Get reads and validates one record.
func (r AutonomyRepository) Get(_ context.Context, id domain.AutonomyID) (domain.RunbookAutonomy, error) {
	var snapshot domain.RunbookAutonomySnapshot
	if err := r.files.read(id.String(), &snapshot); err != nil {
		return domain.RunbookAutonomy{}, err
	}
	return domain.RestoreRunbookAutonomy(snapshot)
}

// List reads every record, in id order.
func (r AutonomyRepository) List(ctx context.Context) ([]domain.RunbookAutonomy, error) {
	ids, err := r.files.ids()
	if err != nil {
		return nil, err
	}
	records := make([]domain.RunbookAutonomy, 0, len(ids))
	for _, id := range ids {
		record, err := r.Get(ctx, domain.AutonomyID(id))
		if r.files.skipListed(id, err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}
