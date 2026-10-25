package domain

import "time"

// DelegationGrant is what a delegated task was given out of its parent's
// task: at most so many model calls and tool calls (0: no limit of the
// parent's, the bot's own apply) and a deadline it must end by.
type DelegationGrant struct {
	MaxModelCalls int       `json:"maxModelCalls,omitempty"`
	MaxToolCalls  int       `json:"maxToolCalls,omitempty"`
	Deadline      time.Time `json:"deadline"`
}

// Validate checks the grant's invariants.
func (g DelegationGrant) Validate() error {
	if g.MaxModelCalls < 0 || g.MaxToolCalls < 0 {
		return invalid("delegation grant", "limits must not be negative")
	}
	if g.Deadline.IsZero() {
		return invalid("delegation grant", "needs a deadline")
	}
	return nil
}
