package domain

import "fmt"

// ConflictError says an aggregate with that identifier already exists
// or, with a Reason, that a write would overwrite a newer state of it.
type ConflictError struct {
	Kind string
	ID   string
	// Reason, when set, replaces "already exists" in the message.
	Reason string
}

func (e ConflictError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("%s %s %s", e.Kind, e.ID, e.Reason)
	}
	return fmt.Sprintf("%s %s already exists", e.Kind, e.ID)
}
