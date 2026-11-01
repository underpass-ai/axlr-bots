package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// incidentAdvancer moves an incident on when its triage task ends: a
// completed task is the diagnosis, a task that failed, expired or was
// cancelled escalates it to a person, and every execution's cost is
// added, a delegated child's included. Only the task of the incident's current cycle moves it; an
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
	if !task.Status().IsTerminal() {
		return nil
	}
	id := task.IncidentID()
	if id == "" {
		return a.childEnded(ctx, task, execution)
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

// maxCostLineage bounds the walk from a delegated task up to the triage
// task of its incident.
const maxCostLineage = 8

// childEnded adds a delegated task's cost to the incident of the triage
// task it descends from; nothing else of the incident changes. On 31
// October 2026 an incident's cost counted only triage's own executions,
// and its diag-k8s children were summed by hand.
func (a incidentAdvancer) childEnded(ctx context.Context, task domain.Task, execution domain.Execution) error {
	usd := execution.Usage().CostUSD
	if usd <= 0 {
		return nil
	}
	current := task
	for depth := 0; depth < maxCostLineage; depth++ {
		lineage, ok := current.Lineage()
		if !ok || lineage.EventID != "" {
			// Not delegated, or made by a teammate's event: no incident's
			// work.
			return nil
		}
		parentID, err := domain.NewTaskID(lineage.ParentTaskID)
		if err != nil {
			return nil
		}
		parent, err := a.deps.Tasks.Get(ctx, parentID)
		if err != nil {
			if domain.IsNotFound(err) {
				return nil
			}
			return err
		}
		id := parent.IncidentID()
		if id == "" {
			current = parent
			continue
		}
		incident, err := a.deps.Incidents.Get(ctx, id)
		if err != nil {
			if domain.IsNotFound(err) {
				return nil
			}
			return err
		}
		if err := incident.AddCost(usd, a.deps.Clock.Now()); err != nil {
			return err
		}
		if err := a.deps.Incidents.Save(ctx, incident); err != nil {
			return err
		}
		return a.rec.audit(ctx, "incident.cost", id.String(), domain.AuditOK, map[string]string{"task": task.ID().String(), "fromTask": parent.ID().String()})
	}
	return nil
}
