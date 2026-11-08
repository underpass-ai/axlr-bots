package filestore

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/domain"
)

// AutonomyRepository keeps each runbook's standing under
// <state>/autonomy/<id>.json. Unlike the other records, a file it cannot
// read is never set aside: a read that moved it would forget a lowering
// and start the runbook over. It stays for a person, a Get of it is an
// error, and the apply takes the card.
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

// Get reads and validates one record; a file that does not validate is
// an error, never a not-found.
func (r AutonomyRepository) Get(_ context.Context, id domain.AutonomyID) (domain.RunbookAutonomy, error) {
	var snapshot domain.RunbookAutonomySnapshot
	if err := r.files.read(id.String(), &snapshot); err != nil {
		return domain.RunbookAutonomy{}, err
	}
	return domain.RestoreRunbookAutonomy(snapshot)
}

// List reads every record it can, in id order. One it cannot read is
// logged and left where it is.
func (r AutonomyRepository) List(ctx context.Context) ([]domain.RunbookAutonomy, error) {
	ids, err := r.files.ids()
	if err != nil {
		return nil, err
	}
	records := make([]domain.RunbookAutonomy, 0, len(ids))
	for _, id := range ids {
		record, err := r.Get(ctx, domain.AutonomyID(id))
		if domain.IsNotFound(err) {
			continue
		}
		if err != nil {
			log.Printf("filestore: autonomy %s cannot be read and was left in place: %v", id, err)
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

// Delete removes the record; one that is gone is no error.
func (r AutonomyRepository) Delete(_ context.Context, id domain.AutonomyID) error {
	path, err := r.files.path(id.String())
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
