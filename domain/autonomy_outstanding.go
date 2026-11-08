package domain

import "time"

// AutonomyOutstanding is an apply whose end is not recorded yet, as plain
// data: written before it starts, so a lowering that gets lost costs the
// next apply its card and not a count.
type AutonomyOutstanding struct {
	IncidentID string    `json:"incidentId"`
	Cycle      int       `json:"cycle"`
	At         time.Time `json:"at"`
}
