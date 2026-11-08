package application

import "github.com/underpass-ai/axlr-bots/domain"

// IncidentReport is an incident as an operator reads it: the incident,
// whether it waits for a person's decision now (its current task waits
// for an approval where the incident stands, domain.Incident.NeedsPerson)
// and, for one incident read on its own, its current task and the count
// of its tasks' approvals. A list carries NeedsPerson only.
type IncidentReport struct {
	Incident    domain.Incident
	NeedsPerson bool
	// Current is the incident's current task; nil in a list, or when the
	// task is gone.
	Current *domain.Task
	// Approvals counts the decisions its tasks waited for; zero in a list.
	Approvals IncidentApprovals
}
