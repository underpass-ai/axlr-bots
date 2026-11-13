package application

import "github.com/underpass-ai/axlr-bots/domain"

// ProgressCompletion says an execution's progress timeline was cut, or
// that its completion grew: Items append to its completed items.
type ProgressCompletion struct {
	TaskID      domain.TaskID
	ExecutionID domain.ExecutionID
	Cut         ProgressCut
	Items       []ProgressItem
}
