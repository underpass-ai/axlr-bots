package application

import "github.com/underpass-ai/axlr-bots/domain"

// ExecutionReport is how a run ended, as the runtime reported it, the
// policy the adapter ran it under and what it did with the correlation
// (nil when the adapter keeps none).
type ExecutionReport struct {
	Outcome       domain.ExecutionOutcome
	Result        domain.Result
	Usage         domain.Usage
	FailureReason string
	TracePath     string
	Policy        *domain.PolicyRecord
	Correlation   *domain.CorrelationRecord
}
