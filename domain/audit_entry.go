package domain

import "time"

// AuditEntry records who asked the control plane to do what, to which
// subject, and how it ended. Entries are append-only evidence.
type AuditEntry struct {
	ID      string            `json:"id"`
	At      time.Time         `json:"at"`
	Actor   string            `json:"actor"`
	Action  string            `json:"action"`
	Subject string            `json:"subject"`
	Outcome AuditOutcome      `json:"outcome"`
	Details map[string]string `json:"details,omitempty"`
}

// NewAuditEntry validates an audit entry.
func NewAuditEntry(id string, at time.Time, actor, action, subject string, outcome AuditOutcome, details map[string]string) (AuditEntry, error) {
	switch {
	case id == "":
		return AuditEntry{}, invalid("audit id", "must not be empty")
	case at.IsZero():
		return AuditEntry{}, invalid("audit time", "must not be zero")
	case actor == "":
		return AuditEntry{}, invalid("audit actor", "must not be empty")
	case action == "":
		return AuditEntry{}, invalid("audit action", "must not be empty")
	case subject == "":
		return AuditEntry{}, invalid("audit subject", "must not be empty")
	}
	if _, err := ParseAuditOutcome(outcome.String()); err != nil {
		return AuditEntry{}, err
	}
	return AuditEntry{ID: id, At: at, Actor: actor, Action: action, Subject: subject, Outcome: outcome, Details: details}, nil
}
