package axlr

import "encoding/json"

// resultPending is the call a needs_decision result waits on.
type resultPending struct {
	CallID    string          `json:"call_id"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}
