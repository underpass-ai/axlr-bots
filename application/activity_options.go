package application

import "github.com/underpass-ai/axlr-bots/domain"

// DefaultMaxCompletedItems bounds the items completed from transcripts per
// execution when the caller asks for the server's default.
const DefaultMaxCompletedItems = 2000

// MaxActivityEvents bounds the timeline of one task an activity answer
// holds: far past what RecordProgress lets one execution keep.
const MaxActivityEvents = 50000

// ActivityOptions bound what an activity answer holds. The zero value asks
// for everything with the server's defaults.
type ActivityOptions struct {
	// ExcludeChildren answers the task alone.
	ExcludeChildren bool
	// MaxDepth bounds how many levels of children are followed; zero is
	// the delegation limit.
	MaxDepth int
	// SkipCompletedProgress marks cuts but does not read transcripts.
	SkipCompletedProgress bool
	// MaxCompletedItems bounds the items completed per execution; zero is
	// DefaultMaxCompletedItems.
	MaxCompletedItems int
}

// depth is the levels of children to follow.
func (o ActivityOptions) depth() int {
	switch {
	case o.ExcludeChildren:
		return 0
	case o.MaxDepth <= 0 || o.MaxDepth > domain.MaxDelegationDepthLimit:
		return domain.MaxDelegationDepthLimit
	}
	return o.MaxDepth
}

// completedLimit is the items completed per execution.
func (o ActivityOptions) completedLimit() int {
	if o.MaxCompletedItems <= 0 {
		return DefaultMaxCompletedItems
	}
	return o.MaxCompletedItems
}
