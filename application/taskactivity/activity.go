package taskactivity

import "github.com/underpass-ai/axlr-bots/domain"

// Activity is one task of the tree with everything under it.
type Activity struct {
	Task       domain.TaskSnapshot `json:"task"`
	Executions []Execution         `json:"executions"`
	// Events is the task's own timeline, oldest first.
	Events   []domain.Event `json:"events"`
	Children []Activity     `json:"children"`
	// Subtree sums the usage of the task's executions and every
	// descendant's; CostUnknown is true when any of them is unknown.
	Subtree domain.Usage `json:"subtreeUsage"`
}
