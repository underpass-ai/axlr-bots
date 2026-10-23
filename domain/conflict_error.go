package domain

import "fmt"

// ConflictError says an aggregate with that identifier already exists.
type ConflictError struct {
	Kind string
	ID   string
}

func (e ConflictError) Error() string {
	return fmt.Sprintf("%s %s already exists", e.Kind, e.ID)
}
