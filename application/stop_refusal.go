package application

// StopRefusal is a task Stop could not cancel, and why.
type StopRefusal struct {
	TaskID string `json:"taskId"`
	Reason string `json:"reason"`
}
