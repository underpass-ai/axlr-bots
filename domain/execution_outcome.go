package domain

// ExecutionOutcome is how one attempt at a task ended, or that it is still
// running. It is read from the runtime's result, never from its traces.
type ExecutionOutcome string

const (
	// ExecutionRunning is an attempt in progress.
	ExecutionRunning ExecutionOutcome = "Running"
	// ExecutionCompleted is an attempt that produced an answer.
	ExecutionCompleted ExecutionOutcome = "Completed"
	// ExecutionFailed is an attempt that ended in an error or a timeout.
	ExecutionFailed ExecutionOutcome = "Failed"
	// ExecutionDecisionRequired is an attempt stopped on a tool call that
	// needs a person; the runtime session keeps the pending call.
	ExecutionDecisionRequired ExecutionOutcome = "DecisionRequired"
	// ExecutionCancelled is an attempt the operator stopped.
	ExecutionCancelled ExecutionOutcome = "Cancelled"
)

// ParseExecutionOutcome validates an outcome.
func ParseExecutionOutcome(raw string) (ExecutionOutcome, error) {
	switch ExecutionOutcome(raw) {
	case ExecutionRunning, ExecutionCompleted, ExecutionFailed, ExecutionDecisionRequired, ExecutionCancelled:
		return ExecutionOutcome(raw), nil
	}
	return "", invalid("execution outcome", "must be Running, Completed, Failed, DecisionRequired or Cancelled")
}

// IsFinal reports whether the attempt has ended.
func (o ExecutionOutcome) IsFinal() bool { return o != ExecutionRunning }

func (o ExecutionOutcome) String() string { return string(o) }
