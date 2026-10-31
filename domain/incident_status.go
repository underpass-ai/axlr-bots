package domain

// IncidentStatus is where an incident is in its life. Phase 0 knows four
// states; the states between a diagnosis and a resolution (proposed,
// applying, verifying) come with the bot that applies a fix.
type IncidentStatus string

const (
	// IncidentOpen is an incident whose cause nobody has said yet: its
	// triage task is pending or running.
	IncidentOpen IncidentStatus = "Open"
	// IncidentDiagnosed is an incident whose triage task completed with
	// the cause and its evidence.
	IncidentDiagnosed IncidentStatus = "Diagnosed"
	// IncidentResolved is an incident whose alerts went green, or that a
	// person closed.
	IncidentResolved IncidentStatus = "Resolved"
	// IncidentEscalated is an incident the bots gave up on: the triage
	// task failed, expired or was cancelled, and a person must look.
	IncidentEscalated IncidentStatus = "Escalated"
)

// ParseIncidentStatus validates an incident status.
func ParseIncidentStatus(raw string) (IncidentStatus, error) {
	switch IncidentStatus(raw) {
	case IncidentOpen, IncidentDiagnosed, IncidentResolved, IncidentEscalated:
		return IncidentStatus(raw), nil
	}
	return "", invalid("incident status", "must be Open, Diagnosed, Resolved or Escalated")
}

// IsResolved reports an incident that is closed: a new alert reopens it
// or opens another.
func (s IncidentStatus) IsResolved() bool { return s == IncidentResolved }

func (s IncidentStatus) String() string { return string(s) }
