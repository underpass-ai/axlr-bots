package domain

// TaskID identifies a task.
type TaskID string

// NewTaskID validates a task identifier.
func NewTaskID(raw string) (TaskID, error) {
	value, err := validateIdentifier("task id", raw)
	if err != nil {
		return "", err
	}
	return TaskID(value), nil
}

func (id TaskID) String() string { return string(id) }
