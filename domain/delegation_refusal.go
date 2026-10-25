package domain

// Reasons a delegation is refused, in the order the control plane
// checks them.
const (
	RefusedNotAllowed        = "not_allowed"
	RefusedTargetUnavailable = "target_unavailable"
	RefusedDepthExceeded     = "depth_exceeded"
	RefusedCycle             = "cycle"
	RefusedBudgetExhausted   = "budget_exhausted"
)

// DelegationRefusal is a delegation the control plane did not create:
// which check refused it and why, in words the model can act on.
type DelegationRefusal struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}
