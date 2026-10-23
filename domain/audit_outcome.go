package domain

// AuditOutcome says how an audited action ended.
type AuditOutcome string

const (
	// AuditOK is an action that was carried out.
	AuditOK AuditOutcome = "ok"
	// AuditDenied is an action a rule refused.
	AuditDenied AuditOutcome = "denied"
	// AuditFailed is an action that was attempted and failed.
	AuditFailed AuditOutcome = "failed"
)

// ParseAuditOutcome validates an audit outcome.
func ParseAuditOutcome(raw string) (AuditOutcome, error) {
	switch AuditOutcome(raw) {
	case AuditOK, AuditDenied, AuditFailed:
		return AuditOutcome(raw), nil
	}
	return "", invalid("audit outcome", "must be ok, denied or failed")
}

func (o AuditOutcome) String() string { return string(o) }
