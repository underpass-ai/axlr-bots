package unixapi

// DecideRequest decides the pending call of a WaitingApproval task;
// CallID, when given, must name that call, so an approve link for one
// call never decides another.
type DecideRequest struct {
	Decision string `json:"decision"`
	CallID   string `json:"callId,omitempty"`
}
