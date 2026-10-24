package application

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// CreateEnrollmentToken mints a one-time token for a node that is not
// there yet. The operator carries the token to the new host; the node
// presents it once, with its certificate signing request, and gets its
// certificate. The control plane stores the hash of the secret only.
type CreateEnrollmentToken struct {
	deps Dependencies
	rec  recorder
}

// NewCreateEnrollmentToken wires the use case.
func NewCreateEnrollmentToken(deps Dependencies) CreateEnrollmentToken {
	return CreateEnrollmentToken{deps: deps, rec: recorder{deps: deps}}
}

// Execute mints the token for the node id (a generated one when empty),
// with the labels, living ttl (DefaultEnrollmentTTL when zero).
func (uc CreateEnrollmentToken) Execute(ctx context.Context, nodeID string, labels map[string]string, ttl time.Duration) (EnrollmentToken, error) {
	if nodeID == "" {
		nodeID = uc.deps.IDs.NewID("node-")
	}
	id, err := domain.NewNodeID(nodeID)
	if err != nil {
		return EnrollmentToken{}, err
	}
	nodeLabels, err := domain.NewNodeLabels(labels)
	if err != nil {
		return EnrollmentToken{}, err
	}
	if ttl == 0 {
		ttl = domain.DefaultEnrollmentTTL
	}
	secret, err := newSecret()
	if err != nil {
		return EnrollmentToken{}, err
	}
	tokenID, err := domain.NewEnrollmentID(uc.deps.IDs.NewID("enr-"))
	if err != nil {
		return EnrollmentToken{}, err
	}
	now := uc.deps.Clock.Now()
	enrollment, err := domain.NewEnrollment(tokenID, id, nodeLabels, secret, now, ttl)
	if err != nil {
		return EnrollmentToken{}, err
	}
	ca, err := uc.deps.Certifier.CACertificate(ctx)
	if err != nil {
		return EnrollmentToken{}, err
	}
	if err := uc.deps.Enrollments.Save(ctx, enrollment); err != nil {
		return EnrollmentToken{}, err
	}
	details := map[string]string{"token": tokenID.String(), "expiresAt": enrollment.ExpiresAt().UTC().Format(time.RFC3339)}
	if err := uc.rec.audit(ctx, "node.enroll-token", id.String(), domain.AuditOK, details); err != nil {
		return EnrollmentToken{}, err
	}
	return EnrollmentToken{ID: tokenID, Secret: secret, NodeID: id, Labels: nodeLabels.Copy(), ExpiresAt: enrollment.ExpiresAt(), CAPEM: ca}, nil
}

// newSecret draws 32 random bytes, URL-safe base64.
func newSecret() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("enrollment secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
