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
}

func (e ForbiddenError) Error() string {
	return fmt.Sprintf("forbidden: %s", e.Reason)
}

// IsForbidden reports whether err is, or wraps, a ForbiddenError.
func IsForbidden(err error) bool {
	var target ForbiddenError
	return errors.As(err, &target)
}
