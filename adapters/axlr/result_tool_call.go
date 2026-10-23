package axlr

// resultToolCall is one call of the session as the result lists it.
type resultToolCall struct {
	CallID   string `json:"call_id"`
	Tool     string `json:"tool"`
	Decision string `json:"decision,omitempty"`
	Status   string `json:"status"`
}
