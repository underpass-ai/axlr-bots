package application

// DecideRequest decides the pending call of a WaitingApproval task;
// CallID, when given, must name that call, so an approve link for one
// call never decides another. DecidedBy is who decides, as the token
// holder asserts it (the X-Axlr-Actor header says the same on every
// decide route); empty is the control plane's own actor, which a bot
// that lists approvers refuses.
type DecideRequest struct {
	Decision  string `json:"decision"`
	CallID    string `json:"callId,omitempty"`
	DecidedBy string `json:"decidedBy,omitempty"`
}
