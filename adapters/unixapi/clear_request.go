package unixapi

// ClearRequest ends a runbook's outstanding applies by a person's hand:
// who is clearedBy or the X-Axlr-Actor header (both given must agree),
// and why.
type ClearRequest struct {
	Reason    string `json:"reason,omitempty"`
	ClearedBy string `json:"clearedBy,omitempty"`
}
