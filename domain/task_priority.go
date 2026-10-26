package domain

// TaskPriority says whether a task waits its turn or goes first. Only the
// operator makes a task urgent: at submission, or in the definition of
// the bot that receives a teammate's event. An urgent task is assigned
// before the bot's other tasks and cancels the ones in progress.
type TaskPriority string

const (
	// PriorityNormal waits its turn, oldest first.
	PriorityNormal TaskPriority = "normal"
	// PriorityUrgent goes first and preempts the bot's work in progress.
	PriorityUrgent TaskPriority = "urgent"
)

// ParseTaskPriority validates a priority; empty is normal.
func ParseTaskPriority(raw string) (TaskPriority, error) {
	switch TaskPriority(raw) {
	case "", PriorityNormal:
		return PriorityNormal, nil
	case PriorityUrgent:
		return PriorityUrgent, nil
	}
	return "", invalid("priority", "must be normal or urgent")
}

func (p TaskPriority) String() string { return string(p) }
