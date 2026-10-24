package application

import (
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// EnrollmentToken is what CreateEnrollmentToken hands the operator: the
// token's id, its secret (shown once, never stored), the node it is
// for, the labels the node starts with, when it expires and the CA the
// node must trust when it first connects.
type EnrollmentToken struct {
	ID        domain.EnrollmentID
	Secret    string
	NodeID    domain.NodeID
	Labels    map[string]string
	ExpiresAt time.Time
	CAPEM     []byte
}
