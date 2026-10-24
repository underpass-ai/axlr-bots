package application

import "context"

// DecisionExecutor continues an execution that stopped on a tool call a
// person must decide: it hands the decision to the runtime session that
// keeps the pending call, runs the turn to its end and reports how it
// ended, under a new execution.
//
// As with RuntimeExecutor, an error means the decision could not be run or
// observed at all; a turn the runtime reports as failed comes back as a
// report with a Failed outcome and no error.
type DecisionExecutor interface {
	Decide(ctx context.Context, request DecisionRequest) (ExecutionReport, error)
}
