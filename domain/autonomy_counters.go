package domain

// AutonomyCounters are the three counts the ladder climbs by
// (axlr-sre design §8.3). Plain data: it is its own snapshot.
type AutonomyCounters struct {
	Diagnoses int `json:"diagnoses"`
	Proposals int `json:"proposals"`
	Applies   int `json:"applies"`
}

// add counts the signal, when it is one of the three.
func (c *AutonomyCounters) add(signal AutonomySignal) bool {
	switch signal {
	case SignalDiagnosisCorrect:
		c.Diagnoses++
	case SignalProposalApproved:
		c.Proposals++
	case SignalApplyVerified:
		c.Applies++
	default:
		return false
	}
	return true
}

// meets reports all three at or above the thresholds.
func (c AutonomyCounters) meets(t AutonomyThresholds) bool {
	return c.Diagnoses >= t.diagnoses && c.Proposals >= t.proposals && c.Applies >= t.applies
}

func (c AutonomyCounters) valid() bool {
	return c.Diagnoses >= 0 && c.Proposals >= 0 && c.Applies >= 0
}
