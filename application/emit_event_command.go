package application

import "encoding/json"

// EmitEventCommand is what an execution's call of the emit tool asks:
// the event's name, its one-line message, its severity (info when
// empty), the facts for the receivers (a JSON object, at most 8 KiB)
// and, when it is for some teammates only, their ids.
type EmitEventCommand struct {
	Name     string          `json:"name"`
	Message  string          `json:"message"`
	Severity string          `json:"severity,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	To       []string        `json:"to,omitempty"`
}
