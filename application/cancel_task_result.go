package application

import "github.com/underpass-ai/axlr-bots/domain"

// CancelTaskResult is the task after the cancel, the execution it
// stopped (nil when none was running) and whether anything changed: a
// task already Cancelled is answered as it is.
type CancelTaskResult struct {
	Task      domain.Task
	Execution *domain.Execution
	Changed   bool
}
