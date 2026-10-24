package domain

import (
	"bytes"
	"encoding/json"
)

// MaxTaskInputBytes bounds the event a trigger stores on its task.
const MaxTaskInputBytes = 256 << 10

// NewTaskInput validates a task's input: nothing, or valid JSON of at
// most MaxTaskInputBytes, kept compact.
func NewTaskInput(raw []byte) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > MaxTaskInputBytes {
		return nil, invalid("input", "is too large")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, invalid("input", "must be valid JSON")
	}
	return json.RawMessage(compact.Bytes()), nil
}
