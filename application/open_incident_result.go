package application

import "github.com/underpass-ai/axlr-bots/domain"

// OpenIncidentResult is the incident an alert names and what the alert
// did to it: Created for a new incident, Reopened for a new cycle of a
// resolved one, neither for a repeat. Task is the triage task started
// for the incident, nil for a repeat.
type OpenIncidentResult struct {
	Incident domain.Incident
	Task     *domain.Task
	Created  bool
	Reopened bool
	// Action says what the alert did: opened, repeated or reopened.
	Action string
}
