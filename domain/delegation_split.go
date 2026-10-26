package domain

// DelegationSplit is how a task's pool is shared among the children it
// asks for: each one a share of what is left, or each one the same
// share of the whole.
type DelegationSplit string

const (
	// SplitGeometric gives each child the share of what the task has not
	// granted yet, so later children get less.
	SplitGeometric DelegationSplit = "geometric"
	// SplitEven gives each child the share of the whole pool divided by
	// the most children the task may ask for, so every child gets the
	// same.
	SplitEven DelegationSplit = "even"
)

// ParseDelegationSplit validates a split; the empty string is geometric,
// the split a delegation has when it names none.
func ParseDelegationSplit(raw string) (DelegationSplit, error) {
	switch DelegationSplit(raw) {
	case "", SplitGeometric:
		return SplitGeometric, nil
	case SplitEven:
		return SplitEven, nil
	}
	return "", invalid("delegation split", "must be geometric or even")
}

func (s DelegationSplit) String() string { return string(s) }
