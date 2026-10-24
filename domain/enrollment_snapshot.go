package domain

import "time"

// EnrollmentSnapshot is an enrollment token as plain data: the hash of
// the secret, never the secret.
type EnrollmentSnapshot struct {
	ID         string            `json:"id"`
	NodeID     string            `json:"nodeId"`
	Labels     map[string]string `json:"labels,omitempty"`
	SecretHash string            `json:"secretHash"`
	CreatedAt  time.Time         `json:"createdAt"`
	ExpiresAt  time.Time         `json:"expiresAt"`
	UsedAt     *time.Time        `json:"usedAt,omitempty"`
}
