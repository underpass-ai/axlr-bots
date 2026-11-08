package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// incidentStanding says where a task stands in its incident: whether the
// incident still stands on it (its current task, of the current cycle,
// in the phase its status is at, domain.Incident.NeedsPerson) and
// whether a later task has superseded it. A task of no incident, or of
// one that is gone, stands on its own.
type incidentStanding struct {
	deps Dependencies
}

// standsOn reports whether approving the task's call acts on what its
// incident stands on now. A denied apply whose session asks again, or an
// apply of an incident resolved meanwhile, does not: approving it would
// write a proposal the incident no longer stands on (review of #133).
func (s incidentStanding) standsOn(ctx context.Context, task domain.Task) (bool, domain.Incident, error) {
	if task.IncidentID() == "" {
		return true, domain.Incident{}, nil
	}
	incident, err := s.deps.Incidents.Get(ctx, task.IncidentID())
	if domain.IsNotFound(err) {
		return true, domain.Incident{}, nil
	}
	if err != nil {
		return false, domain.Incident{}, err
	}
	return incident.CurrentTaskID() == task.ID() && incident.NeedsPerson(true), incident, nil
}

// superseded reports a task of an incident whose current task is now
// another: a phase task the incident went on without.
func (s incidentStanding) superseded(ctx context.Context, task domain.Task) bool {
	if task.IncidentID() == "" {
		return false
	}
	incident, err := s.deps.Incidents.Get(ctx, task.IncidentID())
	if err != nil {
		return false
	}
	current := incident.CurrentTaskID()
	if current == task.ID() {
		return false
	}
	for _, id := range incident.TaskIDs() {
		if id == task.ID() {
			return true
		}
	}
	return false
}
