package unixapi

import "time"

// EnrollTokenResponse is the token, shown once, and what it is bound to.
type EnrollTokenResponse struct {
	Token     string            `json:"token"`
	ID        string            `json:"id"`
	NodeID    string            `json:"nodeId"`
	Labels    map[string]string `json:"labels,omitempty"`
	ExpiresAt time.Time         `json:"expiresAt"`
}
