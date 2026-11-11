package nodeproto

import "github.com/underpass-ai/axlr-bots/domain"

// ReportAck answers a report: whether this report ended the execution
// (false when it had ended already) and the execution's outcome. For a
// question's task that ended it carries channelMemory, what the answering
// team's store keeps of the question, which the node writes there; an
// older node ignores it.
type ReportAck struct {
	Accepted      bool                          `json:"accepted"`
	Outcome       string                        `json:"outcome"`
	ChannelMemory *domain.ChannelRecordSnapshot `json:"channelMemory,omitempty"`
}
