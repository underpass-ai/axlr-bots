package domain

import "time"

// AutonomyEntry is one line of a runbook's history, as plain data: the
// signal, the incident and cycle it came from, who it is due to, and any
// step it moved. A lowering says what it took back: EarnedBefore is what
// had been earned since the previous lowering. Nothing else is ever
// removed from the history.
type AutonomyEntry struct {
	Signal       string            `json:"signal"`
	IncidentID   string            `json:"incidentId"`
	Cycle        int               `json:"cycle"`
	Actor        string            `json:"actor,omitempty"`
	At           time.Time         `json:"at"`
	From         string            `json:"from,omitempty"`
	To           string            `json:"to,omitempty"`
	EarnedBefore *AutonomyCounters `json:"earnedBefore,omitempty"`
}
