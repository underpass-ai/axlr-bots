package application

import "time"

// RelayedMessage is an operator's message as a node reads it for its
// running execution: the event's id, the cursor of the next read, and
// the text.
type RelayedMessage struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"createdAt"`
}
