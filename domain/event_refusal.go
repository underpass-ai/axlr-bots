package domain

// Reasons an event is refused as a whole; a refused name is
// RefusedNotAllowed, as for a delegation.
const (
	RefusedSeverityNotAllowed = "severity_not_allowed"
	RefusedNoTeam             = "no_team"
	RefusedNotMember          = "not_member"
	RefusedRateLimited        = "rate_limited"
)

// EventRefusal is an event the control plane did not take: which check
// refused it and why, in words the model can act on.
type EventRefusal struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}
