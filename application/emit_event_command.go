package application

import "encoding/json"

// EmitEventCommand is what an execution's call of the emit tool asks:
// the event's name, its one-line message, the facts for the receivers
// (a JSON object, at most 8 KiB) and, when it is for some teammates
// only, their ids. Severity is what an older node still sends: it is
// ignored, the name's kind fixes the severity.
type EmitEventCommand struct {
	Name     string          `json:"name"`
	Message  string          `json:"message"`
	Severity string          `json:"severity,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	To       []string        `json:"to,omitempty"`
}
