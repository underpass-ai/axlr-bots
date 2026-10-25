package domain

import (
	"errors"
	"fmt"
	"time"
)

// ApprovalExpiredError says a decision came after the pending call's
// deadline: it authorises nothing (vision §14), whether the expiry was
// already carried out or not.
type ApprovalExpiredError struct {
	TaskID   string
	CallID   string
	Deadline time.Time
}

func (e ApprovalExpiredError) Error() string {
	call := ""
	if e.CallID != "" {
		call = " (call " + e.CallID + ")"
	}
	return fmt.Sprintf("approval expired: task %s%s had to be decided by %s; the decision authorises nothing", e.TaskID, call, e.Deadline.UTC().Format(time.RFC3339))
}

// IsApprovalExpired reports whether err is, or wraps, an ApprovalExpiredError.
func IsApprovalExpired(err error) bool {
	var target ApprovalExpiredError
	return errors.As(err, &target)
}
