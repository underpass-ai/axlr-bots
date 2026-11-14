package application

import "github.com/underpass-ai/axlr-bots/domain"

// ExecutionActivity is an execution with what is known of its progress.
type ExecutionActivity struct {
	Execution domain.Execution
	// Cut is set when the progress timeline stopped growing.
	Cut *ProgressCut
	// Completed continue the timeline after the cut, oldest first.
	Completed []ProgressItem
}
