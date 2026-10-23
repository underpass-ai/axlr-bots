package application

import "context"

// RuntimeExecutor runs one task through the execution runtime (AXLR) and
// reports how it ended. It is the control plane's only view of the
// runtime: it never reads traces, only the report.
//
// An error means the executor could not run or observe the work at all
// (no binary, no permission); a run that the runtime itself reports as
// failed comes back as a report with a Failed outcome and no error.
type RuntimeExecutor interface {
	Execute(ctx context.Context, request ExecutionRequest) (ExecutionReport, error)
}
