package application

import "github.com/underpass-ai/underpass-bots/domain"

// ExecutionReport is how a run ended, as the runtime reported it.
type ExecutionReport struct {
	Outcome       domain.ExecutionOutcome
	Result        domain.Result
	Usage         domain.Usage
	FailureReason string
	TracePath     string
}
