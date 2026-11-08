package application

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// incidentAdvancer moves an incident on when the task of its current
// phase ends, and when a person decides its apply task's call. A
// completed triage task is the diagnosis and a completed fix task the
// proposal; a person's approval of the apply call moves the incident to
// Applying, and the apply task's completion to Verifying; a verify task
// that says the alert is still red sends it back to Open. A denied or
// expired apply, and a failed one, send it back to Open too (the second
// denial escalates it); a triage, fix or verify task that failed,
// expired or was cancelled escalates it. After each move the control
// plane launches the phase the incident reached. Every execution's cost
// is added, a delegated child's included. Only the incident's current
// task moves it; an incident that is gone, already resolved or moved on
// without the task is left as it is, audited, because the task must
// settle whatever its incident did meanwhile. A task never changes the
// status by itself: the control plane knows which phase launched which
// task (design §4).
type incidentAdvancer struct {
	deps Dependencies
	rec  recorder
}

func newIncidentAdvancer(deps Dependencies) incidentAdvancer {
	return incidentAdvancer{deps: deps, rec: recorder{deps: deps}}
}

// incidentMove is what a task's end or a decision did to its incident:
// the events to publish, in order, with the payload they share.
type incidentMove struct {
	kinds   []domain.EventType
	payload map[string]any
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
		// An earlier task of the incident (a denied apply whose session
		// went on after the new cycle's triage started) moves nothing,
		// but its cost is the incident's.
		if slices.Contains(incident.TaskIDs(), task.ID()) {
			if added, err := incident.AddExecutionCost(execution.ID(), execution.Usage(), a.deps.Clock.Now()); err != nil {
				return err
			} else if added {
				if err := a.deps.Incidents.Save(ctx, incident); err != nil {
					return err
				}
			}
		}
		return a.rec.audit(ctx, "incident.advance", id.String(), domain.AuditDenied, map[string]string{"task": task.ID().String(), "reason": "not the task of the current cycle and phase"})
	}
	phase := current.Phase()
	now := a.deps.Clock.Now()
	_, _ = incident.AddExecutionCost(execution.ID(), execution.Usage(), now)
	from := incident.Status()
	move := incidentMove{payload: map[string]any{"taskId": task.ID().String(), "executionId": execution.ID().String(), "phase": phase.String(), "costUsd": incident.CostUSD()}}
	err = a.end(&incident, current, task, execution, &move, now)
	var state domain.StateError
	if err != nil && !errors.As(err, &state) {
		// What the task wrote was refused even after IncidentText: a
		// person looks at it rather than the incident staying with no
		// task left to move it.
		reason := domain.IncidentText(phase.String()+" answer not usable: "+err.Error(), domain.MaxIncidentReasonLength)
		if err := incident.Escalate(reason, now); err != nil {
			return err
		}
		move.kinds = []domain.EventType{domain.EventIncidentEscalated}
		for _, key := range []string{"diagnosis", "proposal", "proposedBy", "verdict", "from", "sendBacks"} {
			delete(move.payload, key)
		}
		move.payload["reason"] = incident.Reason()
	} else if err != nil {
		// The incident moved on without this task (resolved by the
		// source or by a person, or sent back to Open by a denial): the
		// cost is still kept.
		if saveErr := a.deps.Incidents.Save(ctx, incident); saveErr != nil {
			return saveErr
		}
		return a.rec.audit(ctx, "incident.advance", id.String(), domain.AuditDenied, map[string]string{"task": task.ID().String(), "reason": err.Error()})
	}
	return a.settle(ctx, incident, from, phase, task.ID(), task.CorrelationID(), move)
}

// end makes the move the task's end means for the incident: by the
// phase that launched the task, what the task did, and where the
// incident stands. A move the incident's status refuses is a StateError.
func (a incidentAdvancer) end(incident *domain.Incident, current domain.IncidentTask, task domain.Task, execution domain.Execution, move *incidentMove, now time.Time) error {
	phase := current.Phase()
	completed := task.Status() == domain.TaskCompleted
	answer := execution.Result().Answer
	failure := func() string {
		reason := task.FailureReason()
		if reason == "" {
			reason = "the " + phase.String() + " task was " + task.Status().String()
		}
		return domain.IncidentText(phase.String()+" task "+task.ID().String()+" "+task.Status().String()+": "+reason, domain.MaxIncidentReasonLength)
	}
	switch {
	case phase == domain.IncidentPhaseTriage && completed:
		if answer == "" {
			answer = "the triage task completed without an answer"
		}
		move.kinds = []domain.EventType{domain.EventIncidentDiagnosed}
		move.payload["diagnosis"] = domain.IncidentText(answer, domain.MaxIncidentDiagnosisLength)
		return incident.Diagnose(domain.IncidentText(answer, domain.MaxIncidentDiagnosisLength), now)
	case phase == domain.IncidentPhaseFix && completed:
		if answer == "" {
			answer = "the fix task completed without an answer"
		}
		move.kinds = []domain.EventType{domain.EventIncidentProposed}
		move.payload["proposal"], move.payload["proposedBy"] = domain.IncidentText(answer, domain.MaxIncidentProposalLength), current.BotID().String()
		return incident.Propose(domain.IncidentText(answer, domain.MaxIncidentProposalLength), now)
	case phase == domain.IncidentPhaseApply && completed:
		return a.applied(incident, task, move, now)
	case phase == domain.IncidentPhaseApply && task.Status() == domain.TaskExpired && incident.Status() == domain.IncidentProposed:
		// Nobody decided the apply call in time and the bot's approvals
		// cancel on expiry: an expiry, as a denial is.
		return a.deny(incident, "the apply call of task "+task.ID().String()+" expired before anyone decided it", move, now)
	case phase == domain.IncidentPhaseApply:
		return a.sendBack(incident, failure(), move, now)
	case phase == domain.IncidentPhaseVerify && completed:
		verdict := domain.ParseVerifyVerdict(answer)
		move.payload["verdict"] = verdict.String()
		if !verdict.IsRed() {
			// Green, or no verdict: the source's resolution closes the
			// incident; nothing moves before it.
			return nil
		}
		return a.sendBack(incident, "verify task "+task.ID().String()+" says the alert is still red: "+answer, move, now)
	default:
		move.kinds = []domain.EventType{domain.EventIncidentEscalated}
		move.payload["reason"] = failure()
		return incident.Escalate(failure(), now)
	}
}

// applied moves the incident whose apply task completed: Applying
// becomes Verifying. One still Proposed was applied without a person's
// approval reaching it (a level that asks nobody, or a decision whose
// move was lost): it goes through Applying to Verifying, unless the task
// kept a denied or expired wait, which is a denial.
func (a incidentAdvancer) applied(incident *domain.Incident, task domain.Task, move *incidentMove, now time.Time) error {
	if incident.Status() == domain.IncidentProposed {
		if wait, ok := task.Approval(); ok && (wait.Outcome == domain.ApprovalDenied || wait.Outcome == domain.ApprovalExpired) {
			return a.deny(incident, denialReason(task.ID(), wait.Tool, wait.Outcome, ""), move, now)
		}
		if err := incident.StartApplying(now); err != nil {
			return err
		}
		move.kinds = append(move.kinds, domain.EventIncidentApplying)
	}
	move.kinds = append(move.kinds, domain.EventIncidentVerifying)
	return incident.Verify(now)
}

// deny records a denial of the apply, a send-back.
func (a incidentAdvancer) deny(incident *domain.Incident, reason string, move *incidentMove, now time.Time) error {
	if incident.Status() != domain.IncidentProposed {
		return incident.Deny(reason, now)
	}
	return a.sendBack(incident, reason, move, now)
}

// sendBack sends the incident back to Open with the reason, or escalates
// it at the last send-back (domain.IncidentMaxSendBacks).
func (a incidentAdvancer) sendBack(incident *domain.Incident, reason string, move *incidentMove, now time.Time) error {
	from := incident.Status()
	if err := incident.SendBack(domain.IncidentText(reason, domain.MaxIncidentReasonLength), now); err != nil {
		return err
	}
	move.payload["reason"], move.payload["sendBacks"] = incident.Reason(), incident.SendBacks()
	if incident.Status() == domain.IncidentEscalated {
		move.kinds = []domain.EventType{domain.EventIncidentEscalated}
		return nil
	}
	move.kinds = []domain.EventType{domain.EventIncidentReopened}
	move.payload["from"] = from.String()
	return nil
}

// denialReason says who denied which call of the apply task, or that its
// approval expired.
func denialReason(task domain.TaskID, tool, outcome, by string) string {
	call := "the apply call"
	if tool != "" {
		call += " " + tool
	}
	if outcome == domain.ApprovalExpired {
		return call + " of task " + task.String() + " expired before anyone decided it"
	}
	if by == "" {
		return call + " of task " + task.String() + " was denied"
	}
	return call + " of task " + task.String() + " was denied by " + by
}

// settle saves the moved incident, publishes its events, audits the move
// and launches the phase it reached.
func (a incidentAdvancer) settle(ctx context.Context, incident domain.Incident, from domain.IncidentStatus, phase domain.IncidentPhase, task domain.TaskID, correlation domain.CorrelationID, move incidentMove) error {
	if err := a.deps.Incidents.Save(ctx, incident); err != nil {
		return err
	}
	move.payload["status"] = incident.Status().String()
	sentBack := incident.Status() == domain.IncidentOpen && from != domain.IncidentOpen
	if sentBack {
		move.payload["cycle"] = incident.Cycle()
	}
	for _, kind := range move.kinds {
		if sentBack && kind == domain.EventIncidentReopened {
			// Published with the new cycle's triage task, below.
			continue
		}
		if err := a.rec.incidentEvent(ctx, kind, incident, move.payload); err != nil {
			return err
		}
	}
	details := map[string]string{"task": task.String(), "phase": phase.String(), "status": incident.Status().String()}
	if verdict, ok := move.payload["verdict"].(string); ok {
		details["verdict"] = verdict
	}
	if err := a.rec.audit(ctx, "incident.advance", incident.ID().String(), domain.AuditOK, details); err != nil {
		return err
	}
	if sentBack {
		return a.retriage(ctx, incident, move.payload)
	}
	_, err := newPhaseLauncher(a.deps).launch(ctx, incident, correlation)
	return err
}

// retriage launches the triage of the incident's new cycle at once. One
// that does not start is audited and the reopened event is published
// without a task: the next alert of the group starts it.
func (a incidentAdvancer) retriage(ctx context.Context, incident domain.Incident, payload map[string]any) error {
	extra := map[string]any{}
	for key, value := range payload {
		if key != "taskId" && key != "executionId" {
			extra[key] = value
		}
	}
	extra["endedTaskId"] = payload["taskId"]
	if _, err := NewOpenIncident(a.deps).retriage(ctx, incident, extra); err == nil {
		return nil
	} else if auditErr := a.rec.audit(ctx, "incident.open", incident.ID().String(), domain.AuditDenied, map[string]string{"reason": err.Error(), "cycle": strconv.Itoa(incident.Cycle())}); auditErr != nil {
		return auditErr
	}
	return a.rec.incidentEvent(ctx, domain.EventIncidentReopened, incident, payload)
}

// taskDecided moves the incident of an apply task whose call a person
// decided, or whose approval expired: approved, Proposed becomes
// Applying; denied or expired, the incident goes back to Open (or is
// escalated at the last denial). Only the incident's current apply task
// moves it, and only while it is Proposed: a call decided while Applying
// moves nothing, the task's end does. A store that fails is audited and
// the decision goes on: the task's end moves the incident then.
func (a incidentAdvancer) taskDecided(ctx context.Context, task domain.Task, outcome, callID, tool, by string) {
	id := task.IncidentID()
	if id == "" {
		return
	}
	if err := a.decided(ctx, task, outcome, callID, tool, by); err != nil {
		_ = a.rec.audit(ctx, "incident.advance", id.String(), domain.AuditDenied, map[string]string{"task": task.ID().String(), "decision": outcome, "reason": err.Error()})
	}
}

func (a incidentAdvancer) decided(ctx context.Context, task domain.Task, outcome, callID, tool, by string) error {
	incident, err := a.deps.Incidents.Get(ctx, task.IncidentID())
	if err != nil {
		return err
	}
	current, ok := incident.CurrentTask()
	if !ok || current.TaskID() != task.ID() || current.Phase() != domain.IncidentPhaseApply || incident.Status() != domain.IncidentProposed {
		return nil
	}
	now := a.deps.Clock.Now()
	move := incidentMove{payload: map[string]any{"taskId": task.ID().String(), "phase": domain.IncidentPhaseApply.String(), "callId": callID, "decision": outcome, "decidedBy": by}}
	if tool != "" {
		move.payload["tool"] = tool
	}
	from := incident.Status()
	if outcome == domain.ApprovalApproved {
		if err := incident.StartApplying(now); err != nil {
			return err
		}
		move.kinds = []domain.EventType{domain.EventIncidentApplying}
	} else if err := a.deny(&incident, denialReason(task.ID(), tool, outcome, by), &move, now); err != nil {
		return err
	}
	return a.settle(ctx, incident, from, domain.IncidentPhaseApply, task.ID(), task.CorrelationID(), move)
}

// executionWaited adds the cost of a run of an incident's task that
// stopped on a call for a person: the apply's first run is often its
// dearest, and its task's end would count only the decision's run.
func (a incidentAdvancer) executionWaited(ctx context.Context, task domain.Task, execution domain.Execution) error {
	if task.IncidentID() == "" {
		return nil
	}
	incident, err := a.deps.Incidents.Get(ctx, task.IncidentID())
	if domain.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	added, err := incident.AddExecutionCost(execution.ID(), execution.Usage(), a.deps.Clock.Now())
	if err != nil || !added {
		return err
	}
	return a.deps.Incidents.Save(ctx, incident)
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
