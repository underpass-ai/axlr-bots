package application

import (
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// RelayedMessage is an operator's message as a node reads it for its
// running execution: the event's id, the cursor of the next read, the
// text and, when a teammate's or the operator's event was the message,
// the event's name and data as they were published. A question's answer
// carries Memory, what the asking team's store keeps of the question,
// which the node writes there; an older node ignores it.
type RelayedMessage struct {
	ID        string                        `json:"id"`
	Text      string                        `json:"text"`
	Event     *RelayedEvent                 `json:"event,omitempty"`
	Memory    *domain.ChannelRecordSnapshot `json:"memory,omitempty"`
	CreatedAt time.Time                     `json:"createdAt"`
}

// RelayedEvent is the event behind a message: a Jev program reads the
// name and the data; AXLR reads the text.
type RelayedEvent struct {
	Name string         `json:"name"`
	Data map[string]any `json:"data,omitempty"`
}
