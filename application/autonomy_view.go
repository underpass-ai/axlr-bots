package application

import "github.com/underpass-ai/axlr-bots/domain"

// AutonomyView is a runbook's standing as an operator reads it: the
// record, the thresholds of its ladder (nil when the bot no longer has
// one) and, read through an incident, the verdict on that incident's
// diagnosis in its current cycle. Saved is false for a runbook whose
// first signal has not come yet: the view shows where it starts.
type AutonomyView struct {
	Autonomy      domain.RunbookAutonomy
	Thresholds    *domain.AutonomyThresholdsSnapshot
	DiagnosisMark string
	Saved         bool
}
