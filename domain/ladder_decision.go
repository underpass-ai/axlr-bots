package domain

// LadderDecision is what a ladder says of an apply about to launch. Unit
// is the apply's runbook and namespace when it has them, pinned on its
// task; Grant is the same with Alone set, when the runbook may act alone;
// Reason says why it has its card otherwise, for the audit. A bot without
// a ladder gets the zero value, and an apply exactly as before.
type LadderDecision struct {
	Unit     *LadderGrant
	Grant    *LadderGrant
	Reason   string
	Laddered bool
}
