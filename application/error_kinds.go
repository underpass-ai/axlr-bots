package application

// Kinds of errors the operator and node APIs name.
const (
	KindNotFound     = "not_found"
	KindValidation   = "validation"
	KindState        = "state"
	KindConflict     = "conflict"
	KindForbidden    = "forbidden"
	KindUnauthorized = "unauthorized"
	KindTooLarge     = "too_large"
	// KindApprovalExpired is a decision after the pending call's
	// deadline: it authorises nothing.
	KindApprovalExpired = "approval_expired"
	KindInternal        = "internal"
)
