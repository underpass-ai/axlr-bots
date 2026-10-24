package filestore

import (
	"context"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/domain"
)

// EnrollmentRepository keeps enrollment tokens under
// <state>/enrollments/<id>.json: the hash of each secret, never the
// secret.
type EnrollmentRepository struct {
	files collection
}

// NewEnrollmentRepository stores enrollments under the state directory.
func NewEnrollmentRepository(stateDir string) EnrollmentRepository {
	return EnrollmentRepository{files: collection{dir: filepath.Join(stateDir, "enrollments"), kind: "enrollment"}}
}

// Save writes the enrollment atomically.
func (r EnrollmentRepository) Save(_ context.Context, enrollment domain.Enrollment) error {
	return r.files.write(enrollment.ID().String(), enrollment.Snapshot())
}

// Get reads and validates one enrollment.
func (r EnrollmentRepository) Get(_ context.Context, id domain.EnrollmentID) (domain.Enrollment, error) {
	var snapshot domain.EnrollmentSnapshot
	if err := r.files.read(id.String(), &snapshot); err != nil {
		return domain.Enrollment{}, err
	}
	return domain.RestoreEnrollment(snapshot)
}

// List reads every enrollment, in id order.
func (r EnrollmentRepository) List(ctx context.Context) ([]domain.Enrollment, error) {
	ids, err := r.files.ids()
	if err != nil {
		return nil, err
	}
	enrollments := make([]domain.Enrollment, 0, len(ids))
	for _, id := range ids {
		enrollment, err := r.Get(ctx, domain.EnrollmentID(id))
		if err != nil {
			return nil, err
		}
		enrollments = append(enrollments, enrollment)
	}
	return enrollments, nil
}
