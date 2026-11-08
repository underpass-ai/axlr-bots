package domain

// The thresholds of design §8.3, and the most a definition may ask.
const (
	DefaultDiagnosesThreshold = 5
	DefaultProposalsThreshold = 10
	DefaultAppliesThreshold   = 20
	maxAutonomyThreshold      = 100000
)

// AutonomyThresholds are how many of each signal a runbook needs since
// its last lowering. Only act-within-policy needs all three.
type AutonomyThresholds struct {
	diagnoses int
	proposals int
	applies   int
}

// DefaultAutonomyThresholds is 5 diagnoses, 10 proposals, 20 applies.
func DefaultAutonomyThresholds() AutonomyThresholds {
	return AutonomyThresholds{diagnoses: DefaultDiagnosesThreshold, proposals: DefaultProposalsThreshold, applies: DefaultAppliesThreshold}
}

// NewAutonomyThresholds validates what a definition says.
func NewAutonomyThresholds(s AutonomyThresholdsSnapshot) (AutonomyThresholds, error) {
	t := DefaultAutonomyThresholds()
	for _, field := range []struct {
		name   string
		raw    int
		target *int
	}{{"diagnoses", s.Diagnoses, &t.diagnoses}, {"proposals", s.Proposals, &t.proposals}, {"applies", s.Applies, &t.applies}} {
		if field.raw < 0 || field.raw > maxAutonomyThreshold {
			return AutonomyThresholds{}, invalid("ladder thresholds", field.name+" must be 1 to 100000 (0 or absent is the default)")
		}
		if field.raw > 0 {
			*field.target = field.raw
		}
	}
	return t, nil
}

// Snapshot returns the thresholds as plain data, every one named.
func (t AutonomyThresholds) Snapshot() AutonomyThresholdsSnapshot {
	return AutonomyThresholdsSnapshot{Diagnoses: t.diagnoses, Proposals: t.proposals, Applies: t.applies}
}
