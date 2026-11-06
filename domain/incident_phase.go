package domain

// IncidentPhase is the step of an incident a task works on. Each phase is
// a task of its own, launched by the control plane when the incident
// reaches it, never by a bot that would wait through an approval: triage
// when an alert opens the incident, fix once it is Diagnosed.
type IncidentPhase string

const (
	// IncidentPhaseTriage finds the cause with evidence and changes
	// nothing: its completed task diagnoses the incident.
	IncidentPhaseTriage IncidentPhase = "triage"
	// IncidentPhaseFix proposes the change and its rollback for the
	// diagnosis: its completed task is the incident's proposal.
	IncidentPhaseFix IncidentPhase = "fix"
)

// ParseIncidentPhase validates a phase; empty is triage.
func ParseIncidentPhase(raw string) (IncidentPhase, error) {
	switch IncidentPhase(raw) {
	case "":
		return IncidentPhaseTriage, nil
	case IncidentPhaseTriage, IncidentPhaseFix:
		return IncidentPhase(raw), nil
	}
	return "", invalid("incident phase", "must be triage or fix")
}

func (p IncidentPhase) String() string { return string(p) }
