package domain

import "strings"

// CorrelationID ties the events of one piece of work together (a trace id).
// It may be empty when nothing upstream supplied one.
type CorrelationID string

// NewCorrelationID validates a correlation identifier; empty is allowed.
func NewCorrelationID(raw string) (CorrelationID, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if len(value) > 128 {
		return "", invalid("correlation id", "must be at most 128 characters")
	}
	for _, r := range value {
		if r <= 0x20 || r == 0x7f {
			return "", invalid("correlation id", "must not contain spaces or control characters")
		}
	}
	return CorrelationID(value), nil
}

func (id CorrelationID) String() string { return string(id) }
