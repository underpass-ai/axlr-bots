package application

import (
	"encoding/json"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ExecutionReport is how a run ended, as the runtime reported it, the
// policy the adapter ran it under, what it did with the correlation
// (nil when the adapter keeps none) and the runtime's receipt (nil when
// the build writes none).
type ExecutionReport struct {
	Outcome       domain.ExecutionOutcome
	Result        domain.Result
	Usage         domain.Usage
	FailureReason string
	TracePath     string
	Policy        *domain.PolicyRecord
	Correlation   *domain.CorrelationRecord
	Receipt       json.RawMessage
}
