package domain

import (
	"errors"
	"fmt"
)

// NotFoundError says a repository holds no aggregate with that identifier.
type NotFoundError struct {
	Kind string
	ID   string
}

func (e NotFoundError) Error() string {
	return fmt.Sprintf("%s %s not found", e.Kind, e.ID)
}

// IsNotFound reports whether err is, or wraps, a NotFoundError.
func IsNotFound(err error) bool {
	var target NotFoundError
	return errors.As(err, &target)
}
