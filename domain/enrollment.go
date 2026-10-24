package domain

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"time"
)

// Enrollment is a one-time token an operator mints for a node that is
// not yet there: it is bound to the node id the certificate will carry
// and to the labels the node starts with, it expires, and it is
// consumed by the first enrollment that presents its secret. The
// control plane keeps the hash of the secret; the secret itself is
// printed once and travels with the operator to the new host.
type Enrollment struct {
	id         EnrollmentID
	nodeID     NodeID
	labels     NodeLabels
	secretHash string
	createdAt  time.Time
	expiresAt  time.Time
	usedAt     time.Time
}

// DefaultEnrollmentTTL is how long a token lives when no TTL is given.
const DefaultEnrollmentTTL = time.Hour

// NewEnrollment mints a token for the node that lives until now+ttl.
func NewEnrollment(id EnrollmentID, node NodeID, labels NodeLabels, secret string, now time.Time, ttl time.Duration) (Enrollment, error) {
	switch {
	case id == "":
		return Enrollment{}, invalid("enrollment id", "must not be empty")
	case node == "":
		return Enrollment{}, invalid("node id", "must not be empty")
	case len(secret) < 16:
		return Enrollment{}, invalid("enrollment secret", "must be at least 16 characters")
	case now.IsZero():
		return Enrollment{}, invalid("created at", "must not be zero")
	case ttl <= 0:
		return Enrollment{}, invalid("ttl", "must be positive")
	}
	return Enrollment{id: id, nodeID: node, labels: labels, secretHash: HashEnrollmentSecret(secret), createdAt: now, expiresAt: now.Add(ttl)}, nil
}

// HashEnrollmentSecret is how a token's secret is stored: SHA-256, hex.
func HashEnrollmentSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// ID is the token's identifier, the public half of the token.
func (e Enrollment) ID() EnrollmentID { return e.id }

// NodeID is the node the token enrolls.
func (e Enrollment) NodeID() NodeID { return e.nodeID }

// Labels are the labels the node starts with.
func (e Enrollment) Labels() NodeLabels { return e.labels }

// CreatedAt is when the token was minted.
func (e Enrollment) CreatedAt() time.Time { return e.createdAt }

// ExpiresAt is when the token stops being accepted.
func (e Enrollment) ExpiresAt() time.Time { return e.expiresAt }

// UsedAt is when the token was consumed; zero while it is fresh.
func (e Enrollment) UsedAt() time.Time { return e.usedAt }

// IsUsed reports a consumed token.
func (e Enrollment) IsUsed() bool { return !e.usedAt.IsZero() }

// IsExpired reports a token past its time.
func (e Enrollment) IsExpired(now time.Time) bool { return now.After(e.expiresAt) }

// Consume accepts the secret once: a wrong secret is forbidden, a used
// or expired token is a state error, and a token that passes is marked
// used as of now so a second presentation is refused.
func (e *Enrollment) Consume(secret string, now time.Time) error {
	if e.IsUsed() {
		return StateError{Subject: "enrollment token " + e.id.String(), Current: "used at " + e.usedAt.UTC().Format(time.RFC3339), Attempted: "enroll"}
	}
	if e.IsExpired(now) {
		return StateError{Subject: "enrollment token " + e.id.String(), Current: "expired at " + e.expiresAt.UTC().Format(time.RFC3339), Attempted: "enroll"}
	}
	if subtle.ConstantTimeCompare([]byte(HashEnrollmentSecret(secret)), []byte(e.secretHash)) != 1 {
		return ForbiddenError{Reason: "the enrollment token does not match"}
	}
	e.usedAt = now
	return nil
}

// Snapshot returns the enrollment as plain data.
func (e Enrollment) Snapshot() EnrollmentSnapshot {
	s := EnrollmentSnapshot{ID: e.id.String(), NodeID: e.nodeID.String(), Labels: e.labels.Copy(), SecretHash: e.secretHash, CreatedAt: e.createdAt, ExpiresAt: e.expiresAt}
	if e.IsUsed() {
		used := e.usedAt
		s.UsedAt = &used
	}
	return s
}

// RestoreEnrollment rebuilds an enrollment from a snapshot.
func RestoreEnrollment(s EnrollmentSnapshot) (Enrollment, error) {
	id, err := NewEnrollmentID(s.ID)
	if err != nil {
		return Enrollment{}, err
	}
	node, err := NewNodeID(s.NodeID)
	if err != nil {
		return Enrollment{}, err
	}
	labels, err := NewNodeLabels(s.Labels)
	if err != nil {
		return Enrollment{}, err
	}
	switch {
	case len(s.SecretHash) != 64:
		return Enrollment{}, invalid("secret hash", "must be a SHA-256 in hex")
	case s.CreatedAt.IsZero():
		return Enrollment{}, invalid("created at", "must not be zero")
	case !s.ExpiresAt.After(s.CreatedAt):
		return Enrollment{}, invalid("expires at", "must be after created at")
	}
	e := Enrollment{id: id, nodeID: node, labels: labels, secretHash: s.SecretHash, createdAt: s.CreatedAt, expiresAt: s.ExpiresAt}
	if s.UsedAt != nil {
		e.usedAt = *s.UsedAt
	}
	return e, nil
}
