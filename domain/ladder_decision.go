package domain

// LadderDecision is what a ladder says of an apply about to launch: a
// grant when the runbook may act alone, and in Reason why it keeps its
// card otherwise, for the audit. A bot without a ladder gets the zero
// value, and an apply exactly as before.
type LadderDecision struct {
	Grant    *LadderGrant
	Reason   string
	Laddered bool
}
