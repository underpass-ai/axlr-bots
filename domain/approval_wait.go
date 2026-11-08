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
// ApprovalsNeeded is how many different people must approve the call:
// absent (zero) is one, as before two persons existed; 2 when the bot's
// twoPersons names the task's incident. ApprovedBy lists, for a call
// that waits for two, who approved it and when, in order: the first
// approval is recorded here and runs nothing; the deadline covers the
// whole wait.
type ApprovalWait struct {
	CallID          string         `json:"callId,omitempty"`
	Tool            string         `json:"tool,omitempty"`
	Since           time.Time      `json:"since"`
	Deadline        time.Time      `json:"deadline"`
	OnExpire        ApprovalExpiry `json:"onExpire"`
	Outcome         string         `json:"outcome,omitempty"`
	ClosedAt        *time.Time     `json:"closedAt,omitempty"`
	ApprovalsNeeded int            `json:"approvalsNeeded,omitempty"`
	ApprovedBy      []ApprovalMark `json:"approvedBy,omitempty"`
}

// maxApprovalsNeeded is the most people a call can wait for.
const maxApprovalsNeeded = 2

// Open reports a wait no decision, expiry or cancel has ended.
func (w ApprovalWait) Open() bool { return w.Outcome == "" }

// Needed is how many different people must approve the call: one
// unless the wait says more.
func (w ApprovalWait) Needed() int {
	if w.ApprovalsNeeded < 1 {
		return 1
	}
	return w.ApprovalsNeeded
}

// ApprovedByActor reports whether the actor already approved the call.
func (w ApprovalWait) ApprovedByActor(actor string) bool {
	for _, mark := range w.ApprovedBy {
		if mark.Actor == actor {
			return true
		}
	}
	return false
}

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
	if w.ApprovalsNeeded < 0 || w.ApprovalsNeeded > maxApprovalsNeeded {
		return invalid("approval approvalsNeeded", "must be 1 or 2")
	}
	if len(w.ApprovedBy) > w.Needed() || (w.Open() && len(w.ApprovedBy) >= w.Needed()) {
		return invalid("approval approvedBy", "lists more approvals than the call waits for")
	}
	seen := map[string]bool{}
	for _, mark := range w.ApprovedBy {
		if mark.Actor == "" || mark.At.IsZero() {
			return invalid("approval approvedBy", "needs an actor and a time")
		}
		if seen[mark.Actor] {
			return invalid("approval approvedBy", "repeats "+mark.Actor+": two approvals are two different people")
		}
		seen[mark.Actor] = true
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
