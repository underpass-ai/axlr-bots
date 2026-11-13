package domain

import "time"

// Outcomes of an approval wait.
const (
	ApprovalApproved  = "approved"
	ApprovalDenied    = "denied"
	ApprovalExpired   = "expired"
	ApprovalCancelled = "cancelled"
)

// ApprovalWait is what a WaitingApproval task waits on: the pending call,
// since when, the deadline a person must decide it by and what happens
// at the deadline. Once the wait ends the task keeps it with its outcome
// (approved, denied, expired, cancelled), so a decision that comes too
// late is told from one that never had a call to decide.
type ApprovalWait struct {
	CallID   string         `json:"callId,omitempty"`
	Tool     string         `json:"tool,omitempty"`
	Since    time.Time      `json:"since"`
	Deadline time.Time      `json:"deadline"`
	OnExpire ApprovalExpiry `json:"onExpire"`
	Outcome  string         `json:"outcome,omitempty"`
	ClosedAt *time.Time     `json:"closedAt,omitempty"`
}

// Open reports a wait no decision, expiry or cancel has ended.
func (w ApprovalWait) Open() bool { return w.Outcome == "" }

// Validate checks the wait's invariants.
func (w ApprovalWait) Validate() error {
	if w.Since.IsZero() || w.Deadline.IsZero() {
		return invalid("approval", "needs since and a deadline")
	}
	if !w.Deadline.After(w.Since) {
		return invalid("approval deadline", "must be after the wait began")
	}
	if _, err := ParseApprovalExpiry(w.OnExpire.String()); err != nil {
		return err
	}
	switch w.Outcome {
	case "":
		if w.ClosedAt != nil {
			return invalid("approval", "an open wait has no closedAt")
		}
	case ApprovalApproved, ApprovalDenied, ApprovalExpired, ApprovalCancelled:
		if w.ClosedAt == nil {
			return invalid("approval", "a closed wait needs closedAt")
		}
	default:
		return invalid("approval outcome", "must be approved, denied, expired or cancelled")
	}
	return nil
}
