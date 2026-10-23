// Package domain holds the control plane's model: bots, tasks, executions
// and the events they emit. Values are validated in their constructors and
// aggregates only change through methods that keep their invariants.
package domain

import "fmt"

// ValidationError says a value does not satisfy the invariants of its type.
type ValidationError struct {
	Field  string
	Reason string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Reason)
}

func invalid(field, reason string) error {
	return ValidationError{Field: field, Reason: reason}
}
