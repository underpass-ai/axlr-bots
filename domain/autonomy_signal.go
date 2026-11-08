package domain

// AutonomySignal is one fact the ladder records about a runbook's cycle
// (axlr-sre design §8.3): counted acts of a person, and the failures
// that lower its step.
type AutonomySignal string

const (
	// SignalDiagnosisCorrect is a person marking the cycle's diagnosis
	// correct: one more for the diagnoses counter.
	SignalDiagnosisCorrect AutonomySignal = "diagnosis-correct"
	// SignalDiagnosisWrong is a person marking it wrong: kept in the
	// history; it counts nothing and lowers nothing.
	SignalDiagnosisWrong AutonomySignal = "diagnosis-wrong"
	// SignalProposalApproved is a person approving the apply call: one
	// more for the proposals counter.
	SignalProposalApproved AutonomySignal = "proposal-approved"
	// SignalApplyVerified is a green verify of a cycle whose apply a
	// person approved: one more for the applies counter.
	SignalApplyVerified AutonomySignal = "apply-verified"
	// SignalApplyDenied is a person denying the apply call: one step down.
	SignalApplyDenied AutonomySignal = "apply-denied"
	// SignalVerifyRed is a red verify after an apply: two steps down.
	SignalVerifyRed AutonomySignal = "verify-red"
	// SignalApplyFailed is an apply task that failed once the write was
	// under way (approved, or acting alone): two steps down, the failed
	// rollback of design §8.3.
	SignalApplyFailed AutonomySignal = "apply-failed"
)

// ParseAutonomySignal validates a signal.
func ParseAutonomySignal(raw string) (AutonomySignal, error) {
	switch AutonomySignal(raw) {
	case SignalDiagnosisCorrect, SignalDiagnosisWrong, SignalProposalApproved, SignalApplyVerified, SignalApplyDenied, SignalVerifyRed, SignalApplyFailed:
		return AutonomySignal(raw), nil
	}
	return "", invalid("autonomy signal", "is not one the ladder records: "+raw)
}

// needsPerson reports a signal only a person's named act may carry.
func (s AutonomySignal) needsPerson() bool {
	switch s {
	case SignalDiagnosisCorrect, SignalDiagnosisWrong, SignalProposalApproved, SignalApplyDenied:
		return true
	}
	return false
}

// isDiagnosisMark reports a person's verdict on the cycle's diagnosis.
func (s AutonomySignal) isDiagnosisMark() bool {
	return s == SignalDiagnosisCorrect || s == SignalDiagnosisWrong
}

// lowers is how many steps the signal takes the runbook down.
func (s AutonomySignal) lowers() int {
	switch s {
	case SignalApplyDenied:
		return 1
	case SignalVerifyRed, SignalApplyFailed:
		return 2
	}
	return 0
}

func (s AutonomySignal) String() string { return string(s) }
