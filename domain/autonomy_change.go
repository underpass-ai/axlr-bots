package domain

// AutonomyChange is what recording a signal did: Recorded is false for
// one seen before or one that does not count, and From and To name a
// step it moved (equal when it moved none).
type AutonomyChange struct {
	Recorded bool
	From     AutonomyLevel
	To       AutonomyLevel
}

// Moved reports a signal that changed the step.
func (c AutonomyChange) Moved() bool { return c.From != c.To }
