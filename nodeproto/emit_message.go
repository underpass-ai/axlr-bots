package nodeproto

import "encoding/json"

// EmitMessage is what a node sends when its execution emits an event.
// The emitting bot, its task and its teams are the execution's: the
// control plane reads them from its own records.
type EmitMessage struct {
	Name     string          `json:"name"`
	Message  string          `json:"message"`
	Severity string          `json:"severity,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	To       []string        `json:"to,omitempty"`
}
