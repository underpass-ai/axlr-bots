package application

import "github.com/underpass-ai/axlr-bots/domain"

// ReportResult is the task and the execution after a node's report, and
// whether the report was the one that ended them.
type ReportResult struct {
	Task      domain.Task
	Execution domain.Execution
	Accepted  bool
}
