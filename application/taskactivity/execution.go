package taskactivity

import (
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// Execution is an execution with what it used and what is known about
// its progress.
type Execution struct {
	Snapshot domain.ExecutionSnapshot `json:"execution"`
	// Cut is set when the progress timeline stopped growing.
	Cut *Cut `json:"progressCut,omitempty"`
	// Completed continues the timeline after the cut, from the
	// transcript, oldest first.
	Completed []application.ProgressItem `json:"completedItems,omitempty"`
}
