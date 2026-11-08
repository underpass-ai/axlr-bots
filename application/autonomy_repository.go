package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// AutonomyRepository stores each runbook's standing on its ladder.
type AutonomyRepository interface {
	Save(ctx context.Context, autonomy domain.RunbookAutonomy) error
	// Get returns a domain.NotFoundError when no record has the id.
	Get(ctx context.Context, id domain.AutonomyID) (domain.RunbookAutonomy, error)
	// List answers every record, in no particular order.
	List(ctx context.Context) ([]domain.RunbookAutonomy, error)
	// Delete removes a record; one that is gone is no error.
	Delete(ctx context.Context, id domain.AutonomyID) error
}
