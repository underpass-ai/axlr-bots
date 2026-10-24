package unixapi

// DecideRequest decides the pending call of a WaitingApproval task.
type DecideRequest struct {
	Decision string `json:"decision"`
}
