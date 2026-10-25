package domain

// ApprovalExpiry is what happens to a pending call nobody decided in
// time: it is denied (the session goes on without it, as a person's deny
// would), or the task is cancelled (the session stays where it is).
type ApprovalExpiry string

const (
	// ExpireDeny denies the call: the default.
	ExpireDeny ApprovalExpiry = "deny"
	// ExpireCancel cancels the task.
	ExpireCancel ApprovalExpiry = "cancel"
)

// ParseApprovalExpiry validates the expiry action; empty is deny.
func ParseApprovalExpiry(raw string) (ApprovalExpiry, error) {
	switch ApprovalExpiry(raw) {
	case "", ExpireDeny:
		return ExpireDeny, nil
	case ExpireCancel:
		return ExpireCancel, nil
	}
	return "", invalid("approvals onExpire", "must be deny or cancel")
}

func (e ApprovalExpiry) String() string { return string(e) }
