package application

import "github.com/underpass-ai/axlr-bots/domain"

// SubmitTaskResult is the task a submission names, and whether this
// submission created it or found the earlier one with the same key.
type SubmitTaskResult struct {
	Task    domain.Task
	Created bool
}
