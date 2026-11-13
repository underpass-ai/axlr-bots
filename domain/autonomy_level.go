package domain

// AutonomyLevel is how far a bot may act on its own (vision §10.4): what
// its allowed tools may do without a person. The control plane records
// it on the bot and hands it to the runtime as the execution's policy.
type AutonomyLevel string

const (
	// AutonomyObserve runs read-only tools and read-only host commands.
	AutonomyObserve AutonomyLevel = "observe"
	// AutonomyPropose observes and writes a plan or a diff, nothing else.
	AutonomyPropose AutonomyLevel = "propose"
	// AutonomyActWithApproval runs the allowed writes, each one decided by
	// a person first.
	AutonomyActWithApproval AutonomyLevel = "act-with-approval"
	// AutonomyActWithinPolicy runs the allowed writes without a decision
	// of the control plane's own; the runtime's own rules still apply.
	AutonomyActWithinPolicy AutonomyLevel = "act-within-policy"
)

// ParseAutonomyLevel validates a level; the empty string is observe, the
// level a bot has when its definition names none.
func ParseAutonomyLevel(raw string) (AutonomyLevel, error) {
	switch AutonomyLevel(raw) {
	case "", AutonomyObserve:
		return AutonomyObserve, nil
	case AutonomyPropose, AutonomyActWithApproval, AutonomyActWithinPolicy:
		return AutonomyLevel(raw), nil
	}
	return "", invalid("autonomy level", "must be observe, propose, act-with-approval or act-within-policy")
}

func (l AutonomyLevel) String() string { return string(l) }

// Writes reports a level under which an allowed write may run at all.
func (l AutonomyLevel) Writes() bool {
	return l == AutonomyActWithApproval || l == AutonomyActWithinPolicy
}
