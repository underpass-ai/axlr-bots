package application

import "github.com/underpass-ai/underpass-bots/domain"

// ApproveTaskResult is the task after the decision and the execution that
// carried it to its end.
type ApproveTaskResult struct {
	Task      domain.Task
	Execution domain.Execution
}
