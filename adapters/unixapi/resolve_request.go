package unixapi

// ResolveRequest closes an incident (POST /v1/incidents/{id}/resolve)
// with an optional reason.
type ResolveRequest struct {
	Reason string `json:"reason,omitempty"`
}
