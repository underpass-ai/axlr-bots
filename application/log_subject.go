package application

import "github.com/underpass-ai/axlr-bots/domain"

// LogSubject is who a log line is about: the ids a line that talks of a
// task, an execution or a node carries as fields, so a log store filters
// on them instead of parsing the text. A field left empty is not in the
// line.
type LogSubject struct {
	Bot           domain.BotID
	Node          domain.NodeID
	Task          domain.TaskID
	Execution     domain.ExecutionID
	CorrelationID domain.CorrelationID
}

// Over is s with the fields of more that are set laid over it.
func (s LogSubject) Over(more LogSubject) LogSubject {
	if more.Bot != "" {
		s.Bot = more.Bot
	}
	if more.Node != "" {
		s.Node = more.Node
	}
	if more.Task != "" {
		s.Task = more.Task
	}
	if more.Execution != "" {
		s.Execution = more.Execution
	}
	if more.CorrelationID != "" {
		s.CorrelationID = more.CorrelationID
	}
	return s
}
