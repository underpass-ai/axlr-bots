package application

import "github.com/underpass-ai/axlr-bots/domain"

// TaskActivity is one task of the tree with everything under it.
type TaskActivity struct {
	Task       domain.Task
	Executions []ExecutionActivity
	// Events are the task's own timeline, complete, oldest first.
	Events   []domain.Event
	Children []TaskActivity
	// SubtreeUsage sums the task's executions and every included
	// descendant's; CostUnknown when any is unknown.
	SubtreeUsage domain.Usage
}
