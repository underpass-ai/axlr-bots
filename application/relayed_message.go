package application

import "time"

// RelayedMessage is an operator's message as a node reads it for its
// running execution: the event's id, the cursor of the next read, the
// text and, when a teammate's or the operator's event was the message,
// the event's name and data as they were published.
type RelayedMessage struct {
	ID        string        `json:"id"`
	Text      string        `json:"text"`
	Event     *RelayedEvent `json:"event,omitempty"`
	CreatedAt time.Time     `json:"createdAt"`
}

// RelayedEvent is the event behind a message: a Jev program reads the
// name and the data; AXLR reads the text.
type RelayedEvent struct {
	Name string         `json:"name"`
	Data map[string]any `json:"data,omitempty"`
}
