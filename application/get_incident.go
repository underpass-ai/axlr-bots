package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// GetIncident reads one incident with its current task and the approvals
// its tasks waited for.
type GetIncident struct {
	deps Dependencies
}

// NewGetIncident wires the use case.
func NewGetIncident(deps Dependencies) GetIncident { return GetIncident{deps: deps} }

// Execute returns the incident's report or a domain.NotFoundError. A
// task of the incident that is gone is left out of the count.
func (uc GetIncident) Execute(ctx context.Context, id domain.IncidentID) (IncidentReport, error) {
	incident, err := uc.deps.Incidents.Get(ctx, id)
	if err != nil {
		return IncidentReport{}, err
	}
	report := IncidentReport{Incident: incident, Whole: true}
	// What the ladder says is shown when it can be read: a record that
	// cannot be must not cost the operator the incident.
	if view, err := newLadderKeeper(uc.deps).view(ctx, incident); err == nil {
		report.Autonomy = view
	}
	current := incident.CurrentTaskID()
	for _, taskID := range incident.TaskIDs() {
		task, err := uc.deps.Tasks.Get(ctx, taskID)
		if domain.IsNotFound(err) {
			continue
		}
		if err != nil {
			return IncidentReport{}, err
		}
		report.Approvals.add(task)
		if taskID == current {
			found := task
			report.Current = &found
			report.NeedsPerson = incident.NeedsPerson(task.Status() == domain.TaskWaitingApproval)
		}
	}
	return report, nil
}
