package unixapi

// LowerRequest takes a runbook one step down by a person's hand: who is
// loweredBy or the X-Axlr-Actor header (both given must agree), and why.
type LowerRequest struct {
	Reason    string `json:"reason,omitempty"`
	LoweredBy string `json:"loweredBy,omitempty"`
}
