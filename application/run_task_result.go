package application

import "github.com/underpass-ai/underpass-bots/domain"

// RunTaskResult is the task after its run and the execution that ran it.
type RunTaskResult struct {
	Task      domain.Task
	Execution domain.Execution
}
