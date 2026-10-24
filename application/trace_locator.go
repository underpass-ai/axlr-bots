package application

import "github.com/underpass-ai/axlr-bots/domain"

// TraceLocator finds the trace a runtime adapter kept for an execution, so
// an execution ended by the control plane itself (reconciliation, not the
// runtime's report) can still point at its evidence.
type TraceLocator interface {
	// TracePath returns where the execution's trace is, and false when
	// none was kept.
	TracePath(execution domain.ExecutionID) (string, bool)
}
