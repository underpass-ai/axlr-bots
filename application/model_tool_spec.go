package application

import "encoding/json"

// ModelToolSpec is one tool offered to the model in a proxied call.
type ModelToolSpec struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}
