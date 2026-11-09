package nodeproto

import "encoding/json"

// ModelToolCallMessage is one tool call the model asks for; the node
// judges it with its policy before it runs.
type ModelToolCallMessage struct {
	CallID    string          `json:"callId"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}
