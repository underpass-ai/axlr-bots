package application

// GroupEventsQuery asks for a team's timeline: the team (and its tenant
// when several have the name), the id of the last event the caller has
// (empty: from the newest limit) and at most how many (100 when 0, at
// most 1000).
type GroupEventsQuery struct {
	Name   string
	Tenant string
	After  string
	Limit  int
}
