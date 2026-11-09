package application

import "encoding/json"

// ModelToolCall is a tool call the model asked for.
type ModelToolCall struct {
	CallID    string
	Name      string
	Arguments json.RawMessage
}
