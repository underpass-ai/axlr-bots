package domain

import (
	"errors"
	"fmt"
)

// ForbiddenError says a caller's credentials do not entitle it to what
// it asked: a token that does not match, a certificate that names
// another node or one that was revoked. It is neither a missing
// aggregate nor a bad value: the request was well formed and refused.
type ForbiddenError struct {
	Reason string
	// Revoked says the refusal is for good: the node was revoked, and
	// only a fresh token readmits it. A certificate refused for another
	// reason (one a renewal just superseded) may be retried.
	Revoked bool
}

func (e ForbiddenError) Error() string {
	return fmt.Sprintf("forbidden: %s", e.Reason)
}

// IsForbidden reports whether err is, or wraps, a ForbiddenError.
func IsForbidden(err error) bool {
	var target ForbiddenError
	return errors.As(err, &target)
}

// IsRevoked reports whether err is, or wraps, a ForbiddenError that says
// the node was revoked.
func IsRevoked(err error) bool {
	var target ForbiddenError
	return errors.As(err, &target) && target.Revoked
}
