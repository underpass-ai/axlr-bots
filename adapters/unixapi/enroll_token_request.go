package unixapi

// EnrollTokenRequest asks for a one-time enrollment token: the node id
// the certificate will name (generated when empty), the labels the node
// starts with and how long the token lives (a duration such as 1h; the
// default when empty).
type EnrollTokenRequest struct {
	NodeID string            `json:"nodeId,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
	TTL    string            `json:"ttl,omitempty"`
}
