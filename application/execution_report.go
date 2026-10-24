package application

import "github.com/underpass-ai/axlr-bots/domain"

// ExecutionReport is how a run ended, as the runtime reported it, and
// the policy the adapter ran it under (nil when the adapter keeps none).
type ExecutionReport struct {
	Outcome       domain.ExecutionOutcome
	Result        domain.Result
	Usage         domain.Usage
	FailureReason string
	TracePath     string
	Policy        *domain.PolicyRecord
}
