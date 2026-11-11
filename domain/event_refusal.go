package domain

// Reasons an event is refused as a whole; a refused name is
// RefusedNotAllowed, as for a delegation.
const (
	RefusedNoTeam      = "no_team"
	RefusedNotMember   = "not_member"
	RefusedRateLimited = "rate_limited"
	// RefusedCrossingLimit: the trace already crossed MaxTeamCrossings
	// team boundaries (docs/architecture/team-topics.md).
	RefusedCrossingLimit = "crossing_limit"
	// RefusedNoDomainID: an event that would cross carries no data.id.
	RefusedNoDomainID = "no_domain_id"
)

// EventRefusal is an event the control plane did not take: which check
// refused it and why, in words the model can act on.
type EventRefusal struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}
