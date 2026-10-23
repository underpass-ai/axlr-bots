package domain

import "time"

// Budgets bounds what one task may spend. Zero means "the default", never
// "unlimited": the control plane always applies a timeout.
type Budgets struct {
	maxModelCallsPerTask int
	maxToolCallsPerTask  int
	maxDelegationDepth   int
	taskTimeout          time.Duration
}

// NewBudgets validates budgets; every value must be non-negative.
func NewBudgets(maxModelCalls, maxToolCalls, maxDelegationDepth int, taskTimeout time.Duration) (Budgets, error) {
	switch {
	case maxModelCalls < 0:
		return Budgets{}, invalid("max model calls per task", "must not be negative")
	case maxToolCalls < 0:
		return Budgets{}, invalid("max tool calls per task", "must not be negative")
	case maxDelegationDepth < 0:
		return Budgets{}, invalid("max delegation depth", "must not be negative")
	case taskTimeout < 0:
		return Budgets{}, invalid("task timeout", "must not be negative")
	}
	return Budgets{
		maxModelCallsPerTask: maxModelCalls,
		maxToolCallsPerTask:  maxToolCalls,
		maxDelegationDepth:   maxDelegationDepth,
		taskTimeout:          taskTimeout,
	}, nil
}

// MaxModelCallsPerTask caps model requests in one task; 0 means default.
func (b Budgets) MaxModelCallsPerTask() int { return b.maxModelCallsPerTask }

// MaxToolCallsPerTask caps tool calls in one task; 0 means default.
func (b Budgets) MaxToolCallsPerTask() int { return b.maxToolCallsPerTask }

// MaxDelegationDepth caps how deep a bot may delegate; 0 means default.
func (b Budgets) MaxDelegationDepth() int { return b.maxDelegationDepth }

// TaskTimeout is the wall-clock limit of one task; 0 means default.
func (b Budgets) TaskTimeout() time.Duration { return b.taskTimeout }

// TaskTimeoutOr returns the task timeout, or fallback when none is set.
func (b Budgets) TaskTimeoutOr(fallback time.Duration) time.Duration {
	if b.taskTimeout == 0 {
		return fallback
	}
	return b.taskTimeout
}
