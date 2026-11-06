package application

import (
	"context"
	"errors"

	"github.com/underpass-ai/axlr-bots/domain"
)

// incidentAdvancer moves an incident on when the task of its current
// phase ends. A completed triage task is the diagnosis, and the control
// plane then launches the fix phase; a completed fix task is the
// proposal. A task of either phase that failed, expired or was cancelled
// escalates the incident to a person, and every execution's cost is
// added, a delegated child's included. Only the incident's current task
// moves it; an incident that is gone or already resolved is left as it
// is, audited, because the task must settle whatever its incident did
// meanwhile.
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
	current, ok := incident.CurrentTask()
	if !ok || current.TaskID() != task.ID() {
		return a.rec.audit(ctx, "incident.advance", id.String(), domain.AuditDenied, map[string]string{"task": task.ID().String(), "reason": "not the task of the current cycle and phase"})
	}
	phase := current.Phase()
	now := a.deps.Clock.Now()
	_ = incident.AddCost(execution.Usage(), now)
	var (
		kind    domain.EventType
		payload = map[string]any{"taskId": task.ID().String(), "executionId": execution.ID().String(), "phase": phase.String(), "costUsd": incident.CostUSD()}
	)
	switch {
	case task.Status() == domain.TaskCompleted && phase == domain.IncidentPhaseFix:
		answer := execution.Result().Answer
		if answer == "" {
			answer = "the fix task completed without an answer"
		}
		err, kind = incident.Propose(domain.IncidentText(answer, domain.MaxIncidentProposalLength), now), domain.EventIncidentProposed
		payload["proposal"], payload["proposedBy"] = incident.Proposal(), current.BotID().String()
	case task.Status() == domain.TaskCompleted:
		answer := execution.Result().Answer
		if answer == "" {
			answer = "the triage task completed without an answer"
		}
		err, kind = incident.Diagnose(domain.IncidentText(answer, domain.MaxIncidentDiagnosisLength), now), domain.EventIncidentDiagnosed
		payload["diagnosis"] = incident.Diagnosis()
	default:
		reason := task.FailureReason()
		if reason == "" {
			reason = "the " + phase.String() + " task was " + task.Status().String()
		}
		reason = domain.IncidentText(phase.String()+" task "+task.ID().String()+" "+task.Status().String()+": "+reason, domain.MaxIncidentReasonLength)
		err, kind = incident.Escalate(reason, now), domain.EventIncidentEscalated
		payload["reason"] = incident.Reason()
	}
	var state domain.StateError
	if err != nil && !errors.As(err, &state) {
		// What the task wrote was refused even after IncidentText: a
		// person looks at it rather than the incident staying with no
		// task left to move it.
		reason := domain.IncidentText(phase.String()+" answer not usable: "+err.Error(), domain.MaxIncidentReasonLength)
		if err := incident.Escalate(reason, now); err != nil {
			return err
		}
		kind = domain.EventIncidentEscalated
		delete(payload, "diagnosis")
		delete(payload, "proposal")
		delete(payload, "proposedBy")
		payload["reason"] = incident.Reason()
	} else if err != nil {
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
	if err := a.rec.incidentEvent(ctx, kind, incident, payload); err != nil {
		return err
	}
	if err := a.rec.audit(ctx, "incident.advance", id.String(), domain.AuditOK, map[string]string{"task": task.ID().String(), "phase": phase.String(), "status": incident.Status().String()}); err != nil {
		return err
	}
	_, err = newFixPhase(a.deps).launch(ctx, incident, task.CorrelationID())
	return err
}

// lateCost adds the cost an ended execution learnt late (its run
// reported after a cancel) to the task's incident, or to the incident
// of the incident task it descends from; nothing else of the incident
// changes.
func (a incidentAdvancer) lateCost(ctx context.Context, task domain.Task, execution domain.Execution) error {
	usage := execution.Usage()
	id := task.IncidentID()
	if id == "" {
		return a.childEnded(ctx, task, execution)
	}
	if usage.CostUSD <= 0 && !usage.CostUnknown {
		return nil
	}
	incident, err := a.deps.Incidents.Get(ctx, id)
	if domain.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := incident.AddCost(usage, a.deps.Clock.Now()); err != nil {
		return err
	}
	if err := a.deps.Incidents.Save(ctx, incident); err != nil {
		return err
	}
	return a.rec.audit(ctx, "incident.cost", id.String(), domain.AuditOK, map[string]string{"task": task.ID().String(), "execution": execution.ID().String()})
}

// maxCostLineage bounds the walk from a delegated task up to the phase
// task of its incident.
const maxCostLineage = 8

// childEnded adds a delegated task's cost to the incident of the phase
// task it descends from; nothing else of the incident changes. On 31
// October 2026 an incident's cost counted only triage's own executions,
// and its diag-k8s children were summed by hand.
func (a incidentAdvancer) childEnded(ctx context.Context, task domain.Task, execution domain.Execution) error {
	usage := execution.Usage()
	if usage.CostUSD <= 0 && !usage.CostUnknown {
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
		if err := incident.AddCost(usage, a.deps.Clock.Now()); err != nil {
			return err
		}
		if err := a.deps.Incidents.Save(ctx, incident); err != nil {
			return err
		}
		return a.rec.audit(ctx, "incident.cost", id.String(), domain.AuditOK, map[string]string{"task": task.ID().String(), "fromTask": parent.ID().String()})
	}
	return nil
}
