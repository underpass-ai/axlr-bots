package nodeproto

import "encoding/json"

// ModelToolMessage is one tool a node offers the model.
type ModelToolMessage struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}
