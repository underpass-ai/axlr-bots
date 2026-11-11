package application

import "github.com/underpass-ai/axlr-bots/domain"

// ReportResult is the task and the execution after a node's report, and
// whether the report was the one that ended them. ChannelMemory is set
// when the task is a question that ended: what the answering team's store
// keeps of it, which the node writes there
// (docs/architecture/team-channel.md, "Memory"); a repeated report
// carries it again, so a node whose first answer was lost still writes.
type ReportResult struct {
	Task          domain.Task
	Execution     domain.Execution
	Accepted      bool
	ChannelMemory *domain.ChannelRecord
}
