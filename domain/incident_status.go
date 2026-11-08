package domain

// IncidentStatus is where an incident is in its life. The states after a
// proposal (Applying, Verifying) only appear once a teammate takes the
// apply phase.
type IncidentStatus string

const (
	// IncidentOpen is an incident whose cause nobody has said yet: its
	// triage task is pending or running.
	IncidentOpen IncidentStatus = "Open"
	// IncidentDiagnosed is an incident whose triage task completed with
	// the cause and its evidence; its fix task, when a teammate takes
	// the fix phase, is pending or running.
	IncidentDiagnosed IncidentStatus = "Diagnosed"
	// IncidentProposed is an incident whose fix task completed with the
	// change and its rollback, applied by nobody yet. Its apply task, when
	// a teammate takes the apply phase, is pending, running or waiting for
	// a person's approval.
	IncidentProposed IncidentStatus = "Proposed"
	// IncidentApplying is an incident whose apply task a person approved:
	// the change is being carried out.
	IncidentApplying IncidentStatus = "Applying"
	// IncidentVerifying is an incident whose apply task completed: its
	// verify task, when a teammate takes the verify phase, checks the
	// change, and the source's resolution closes it.
	IncidentVerifying IncidentStatus = "Verifying"
	// IncidentResolved is an incident whose alerts went green, or that a
	// person closed.
	IncidentResolved IncidentStatus = "Resolved"
	// IncidentEscalated is an incident the bots gave up on: its triage,
	// fix or verify task failed, expired or was cancelled, or a person
	// denied its apply twice, and a person must look.
	IncidentEscalated IncidentStatus = "Escalated"
)

// ParseIncidentStatus validates an incident status.
func ParseIncidentStatus(raw string) (IncidentStatus, error) {
	switch IncidentStatus(raw) {
	case IncidentOpen, IncidentDiagnosed, IncidentProposed, IncidentApplying, IncidentVerifying, IncidentResolved, IncidentEscalated:
		return IncidentStatus(raw), nil
	}
	return "", invalid("incident status", "must be Open, Diagnosed, Proposed, Applying, Verifying, Resolved or Escalated")
}

// IsResolved reports an incident that is closed: a new alert reopens it
// or opens another.
func (s IncidentStatus) IsResolved() bool { return s == IncidentResolved }

// HasProposal reports a status that stands on a proposal: Proposed,
// Applying and Verifying.
func (s IncidentStatus) HasProposal() bool {
	return s == IncidentProposed || s == IncidentApplying || s == IncidentVerifying
}

func (s IncidentStatus) String() string { return string(s) }
