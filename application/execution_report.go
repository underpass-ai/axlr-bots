package application

import (
	"encoding/json"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ExecutionReport is how a run ended, as the runtime reported it, the
// policy the adapter ran it under, what it did with the correlation
// (nil when the adapter keeps none), the runtime's receipt (nil when
// the build writes none) and what the node read of the team's memory
// before the run.
type ExecutionReport struct {
	Outcome       domain.ExecutionOutcome
	Result        domain.Result
	Usage         domain.Usage
	FailureReason string
	TracePath     string
	Policy        *domain.PolicyRecord
	Correlation   *domain.CorrelationRecord
	Receipt       json.RawMessage
	// ContextRead is what the node read under the run's context keys
	// before it, or why it read nothing; the task keeps it as
	// input.earlier.
	ContextRead []domain.ContextRead
}
