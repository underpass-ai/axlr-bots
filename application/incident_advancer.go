package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// incidentAdvancer moves an incident on when its triage task ends: a
// completed task is the diagnosis, a task that failed, expired or was
// cancelled escalates it to a person, and every execution's cost is
// added. Only the task of the incident's current cycle moves it; an
// incident that is gone or already resolved is left as it is, audited,
// because the task must settle whatever its incident did meanwhile.
type incidentAdvancer struct {
	deps Dependencies
	rec  recorder
}

func newIncidentAdvancer(deps Dependencies) incidentAdvancer {
	return incidentAdvancer{deps: deps, rec: recorder{deps: deps}}
}

// taskEnded advances the task's incident, if it has one and the task is
// over.
func (a incidentAdvancer) taskEnded(ctx context.Context, bot domain.Bot, task domain.Task, execution domain.Execution) error {
	id := task.IncidentID()
	if id == "" || !task.Status().IsTerminal() {
		return nil
	}
	incident, err := a.deps.Incidents.Get(ctx, id)
	if err != nil {
		if domain.IsNotFound(err) {
			return a.rec.audit(ctx, "incident.advance", id.String(), domain.AuditDenied, map[string]string{"task": task.ID().String(), "reason": err.Error()})
		}
		return err
	}
	if incident.CurrentTaskID() != task.ID() {
		return a.rec.audit(ctx, "incident.advance", id.String(), domain.AuditDenied, map[string]string{"task": task.ID().String(), "reason": "not the task of the current cycle"})
	}
	now := a.deps.Clock.Now()
	_ = incident.AddCost(execution.Usage().CostUSD, now)
	var (
		kind    domain.EventType
		payload = map[string]any{"taskId": task.ID().String(), "executionId": execution.ID().String(), "costUsd": incident.CostUSD()}
	)
	switch task.Status() {
	case domain.TaskCompleted:
		answer := execution.Result().Answer
		if answer == "" {
			answer = "the triage task completed without an answer"
		}
		err, kind = incident.Diagnose(answer, now), domain.EventIncidentDiagnosed
		payload["diagnosis"] = incident.Diagnosis()
	default:
		reason := task.FailureReason()
		if reason == "" {
			reason = "the triage task was " + task.Status().String()
		}
		err, kind = incident.Escalate("task "+task.ID().String()+" "+task.Status().String()+": "+reason, now), domain.EventIncidentEscalated
		payload["reason"] = incident.Reason()
	}
	if err != nil {
		// The incident moved on without this task (resolved by the
		// source, or by a person): the cost is still kept.
		if saveErr := a.deps.Incidents.Save(ctx, incident); saveErr != nil {
			return saveErr
		}
		return a.rec.audit(ctx, "incident.advance", id.String(), domain.AuditDenied, map[string]string{"task": task.ID().String(), "reason": err.Error()})
	}
	if err := a.deps.Incidents.Save(ctx, incident); err != nil {
		return err
	}
	payload["status"] = incident.Status().String()
	if err := a.rec.incidentEvent(ctx, kind, bot, incident, payload); err != nil {
		return err
	}
	return a.rec.audit(ctx, "incident.advance", id.String(), domain.AuditOK, map[string]string{"task": task.ID().String(), "status": incident.Status().String()})
}
