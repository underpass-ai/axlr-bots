package domain

// ExecutionKind is what an execution carries: the task's objective as a
// prompt, or a decision on the pending call of an earlier execution.
type ExecutionKind string

const (
	// ExecutionRun runs the task's objective.
	ExecutionRun ExecutionKind = "run"
	// ExecutionDecide decides the pending call and runs the session on.
	ExecutionDecide ExecutionKind = "decide"
)

// ParseExecutionKind validates a kind; empty is run.
func ParseExecutionKind(raw string) (ExecutionKind, error) {
	switch ExecutionKind(raw) {
	case "", ExecutionRun:
		return ExecutionRun, nil
	case ExecutionDecide:
		return ExecutionDecide, nil
	}
	return "", invalid("execution kind", "must be run or decide")
}

func (k ExecutionKind) String() string { return string(k) }
