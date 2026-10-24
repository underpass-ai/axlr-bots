package domain

import "encoding/json"

// MaxTaskInputBytes bounds the event a trigger stores on its task.
const MaxTaskInputBytes = 256 << 10

// NewTaskInput validates a task's input: nothing, or valid JSON of at
// most MaxTaskInputBytes.
func NewTaskInput(raw []byte) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > MaxTaskInputBytes {
		return nil, invalid("input", "is too large")
	}
	if !json.Valid(raw) {
		return nil, invalid("input", "must be valid JSON")
	}
	return json.RawMessage(append([]byte(nil), raw...)), nil
}
