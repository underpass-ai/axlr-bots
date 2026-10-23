package axlr

// eventLine is one line of `axlr-tui --exec --json`: an event of the turn,
// or the result that ends the execution.
type eventLine struct {
	Type     string        `json:"type"`
	Session  string        `json:"session,omitempty"`
	Text     string        `json:"text,omitempty"`
	Tool     string        `json:"tool,omitempty"`
	CallID   string        `json:"call_id,omitempty"`
	Decision string        `json:"decision,omitempty"`
	Status   string        `json:"status,omitempty"`
	Error    string        `json:"error,omitempty"`
	Result   *resultObject `json:"result,omitempty"`
}
