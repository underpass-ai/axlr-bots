package nodeagent

import (
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// nodeLog is the agent's log for lines about the node.
func (a Agent) nodeLog(node domain.NodeID) application.LogPrinter {
	return application.LogWith(a.Options.log(), application.LogSubject{Node: node})
}

// execLog is the agent's log for lines about an execution: its node,
// and the bot, task and correlation id the assignment named.
func (a Agent) execLog(node domain.NodeID, execution domain.ExecutionID) application.LogPrinter {
	subject := a.subjects.of(execution).Over(application.LogSubject{Node: node, Execution: execution})
	return application.LogWith(a.Options.log(), subject)
}
