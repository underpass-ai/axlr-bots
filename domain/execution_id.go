package domain

// ExecutionID identifies a execution.
type ExecutionID string

// NewExecutionID validates a execution identifier.
func NewExecutionID(raw string) (ExecutionID, error) {
	value, err := validateIdentifier("execution id", raw)
	if err != nil {
		return "", err
	}
	return ExecutionID(value), nil
}

func (id ExecutionID) String() string { return string(id) }
