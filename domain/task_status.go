package domain

// TaskStatus is where a task is in its life cycle (vision §8.2).
type TaskStatus string

const (
	// TaskPending is a task accepted but not yet run.
	TaskPending TaskStatus = "Pending"
	// TaskRunning is a task with an execution in progress.
	TaskRunning TaskStatus = "Running"
	// TaskWaitingApproval is a task stopped on a tool call a person must decide.
	TaskWaitingApproval TaskStatus = "WaitingApproval"
	// TaskCompleted is a task whose execution produced a result.
	TaskCompleted TaskStatus = "Completed"
	// TaskFailed is a task whose execution ended in an error.
	TaskFailed TaskStatus = "Failed"
	// TaskCancelled is a task stopped on purpose before completing.
	TaskCancelled TaskStatus = "Cancelled"
)

// ParseTaskStatus validates a task status.
func ParseTaskStatus(raw string) (TaskStatus, error) {
	switch TaskStatus(raw) {
	case TaskPending, TaskRunning, TaskWaitingApproval, TaskCompleted, TaskFailed, TaskCancelled:
		return TaskStatus(raw), nil
	}
	return "", invalid("task status", "must be Pending, Running, WaitingApproval, Completed, Failed or Cancelled")
}

// IsTerminal reports whether the task can change no further.
func (s TaskStatus) IsTerminal() bool {
	return s == TaskCompleted || s == TaskFailed || s == TaskCancelled
}

func (s TaskStatus) String() string { return string(s) }
