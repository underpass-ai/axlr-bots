package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// EnrollmentRepository stores enrollment tokens durably: the hash of the
// secret, the node they are for, whether they were used.
type EnrollmentRepository interface {
	Save(ctx context.Context, enrollment domain.Enrollment) error
	// Get returns a domain.NotFoundError when no token has the id.
	Get(ctx context.Context, id domain.EnrollmentID) (domain.Enrollment, error)
	List(ctx context.Context) ([]domain.Enrollment, error)
}
