package domain

// IncidentPhase is the step of an incident a task works on. Each phase is
// a task of its own, launched by the control plane when the incident
// reaches it, never by a bot that would wait through an approval: triage
// when an alert opens the incident, fix once it is Diagnosed, apply once
// it is Proposed, verify once the apply task completed.
type IncidentPhase string

const (
	// IncidentPhaseTriage finds the cause with evidence and changes
	// nothing: its completed task diagnoses the incident.
	IncidentPhaseTriage IncidentPhase = "triage"
	// IncidentPhaseFix proposes the change and its rollback for the
	// diagnosis: its completed task is the incident's proposal.
	IncidentPhaseFix IncidentPhase = "fix"
	// IncidentPhaseApply carries the proposal out: its write call waits
	// for a person's approval, which moves the incident to Applying, and
	// its completed task moves it to Verifying.
	IncidentPhaseApply IncidentPhase = "apply"
	// IncidentPhaseVerify checks that the applied change took: a
	// completed task that says the alert is still red sends the incident
	// back to Open; otherwise the source's resolution closes it.
	IncidentPhaseVerify IncidentPhase = "verify"
)

// ParseIncidentPhase validates a phase; empty is triage.
func ParseIncidentPhase(raw string) (IncidentPhase, error) {
	switch IncidentPhase(raw) {
	case "":
		return IncidentPhaseTriage, nil
	case IncidentPhaseTriage, IncidentPhaseFix, IncidentPhaseApply, IncidentPhaseVerify:
		return IncidentPhase(raw), nil
	}
	return "", invalid("incident phase", "must be triage, fix, apply or verify")
}

// LaunchedAt is the status the incident has when the control plane
// launches the phase's task: Open for triage, Diagnosed for fix,
// Proposed for apply, Verifying for verify.
func (p IncidentPhase) LaunchedAt() IncidentStatus {
	switch p {
	case IncidentPhaseFix:
		return IncidentDiagnosed
	case IncidentPhaseApply:
		return IncidentProposed
	case IncidentPhaseVerify:
		return IncidentVerifying
	}
	return IncidentOpen
}

func (p IncidentPhase) String() string { return string(p) }
