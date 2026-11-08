package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// DiagnosisCorrect and DiagnosisWrong are the marks a person may give
// a diagnosis.
const (
	DiagnosisCorrect = "correct"
	DiagnosisWrong   = "wrong"
)

// MarkDiagnosis records a person's verdict on the diagnosis of the
// incident's current cycle (axlr-sre design §8.3): correct counts toward
// the runbook's diagnoses, wrong is kept and changes nothing. The actor
// must be named, a person (no policy, no control plane) and one of the
// apply bot's approvers. Once per cycle: the same verdict again changes
// nothing, the other is refused.
type MarkDiagnosis struct {
	deps Dependencies
	rec  recorder
}

// NewMarkDiagnosis wires the use case.
func NewMarkDiagnosis(deps Dependencies) MarkDiagnosis {
	return MarkDiagnosis{deps: deps, rec: recorder{deps: deps}}
}

// Execute marks the diagnosis and returns the runbook's standing.
func (uc MarkDiagnosis) Execute(ctx context.Context, id domain.IncidentID, verdict, actor string) (AutonomyView, error) {
	signal, err := diagnosisSignal(verdict)
	if err != nil {
		return AutonomyView{}, err
	}
	incident, err := uc.deps.Incidents.Get(ctx, id)
	if err != nil {
		return AutonomyView{}, err
	}
	keeper := newLadderKeeper(uc.deps)
	taker, trigger, ok, err := keeper.takerOf(ctx, incident)
	if err != nil {
		return AutonomyView{}, err
	}
	if !ok {
		return AutonomyView{}, uc.refuse(ctx, id, actor, domain.StateError{Subject: "incident " + id.String(), Current: "on no autonomy ladder", Attempted: "mark its diagnosis"})
	}
	unit, reason, ok := domain.LadderUnitOf(trigger, incident)
	if !ok {
		if reason == "" {
			reason = "its apply bot keeps no ladder"
		}
		return AutonomyView{}, uc.refuse(ctx, id, actor, domain.StateError{Subject: "incident " + id.String(), Current: "on no autonomy ladder: " + reason, Attempted: "mark its diagnosis"})
	}
	if err := uc.mayMark(taker, actor); err != nil {
		return AutonomyView{}, uc.refuse(ctx, id, actor, err)
	}
	diagnosed, err := uc.diagnosed(ctx, incident)
	if err != nil {
		return AutonomyView{}, err
	}
	if !diagnosed {
		return AutonomyView{}, uc.refuse(ctx, id, actor, domain.StateError{Subject: "incident " + id.String(), Current: "no diagnosis in cycle " + incident.Status().String(), Attempted: "mark its diagnosis"})
	}
	if _, err := keeper.apply(ctx, taker, trigger, unit, incident, incident.Cycle(), signal, actor); err != nil {
		return AutonomyView{}, uc.refuse(ctx, id, actor, err)
	}
	view, err := keeper.view(ctx, incident)
	if err != nil {
		return AutonomyView{}, err
	}
	return *view, nil
}

func diagnosisSignal(verdict string) (domain.AutonomySignal, error) {
	switch verdict {
	case DiagnosisCorrect:
		return domain.SignalDiagnosisCorrect, nil
	case DiagnosisWrong:
		return domain.SignalDiagnosisWrong, nil
	}
	return "", domain.ValidationError{Field: "verdict", Reason: "must be correct or wrong"}
}

// mayMark refuses who cannot speak for the diagnosis: nobody named, a
// policy, the control plane's own name on a daemon, or an actor the
// apply bot's approvers do not list.
func (uc MarkDiagnosis) mayMark(taker domain.Bot, actor string) error {
	name, err := domain.ValidateActor("actor", actor)
	if err != nil {
		return domain.ForbiddenError{Reason: "a diagnosis is marked by a named person: name who marks it (--actor, X-Axlr-Actor or decidedBy)"}
	}
	if !personName(uc.deps, name) {
		return domain.ForbiddenError{Reason: name + " is not a person's name: a person marks a diagnosis under their own"}
	}
	if !taker.Approvals().MayDecide(name) {
		return domain.ForbiddenError{Reason: name + " is not among the approvers of bot " + taker.ID().String()}
	}
	return nil
}

// diagnosed reports a triage task of the incident's current cycle that
// completed: the diagnosis it shows is that cycle's.
func (uc MarkDiagnosis) diagnosed(ctx context.Context, incident domain.Incident) (bool, error) {
	for _, attached := range incident.Tasks() {
		if attached.Phase() != domain.IncidentPhaseTriage || attached.Cycle() != incident.Cycle() {
			continue
		}
		task, err := uc.deps.Tasks.Get(ctx, attached.TaskID())
		if err != nil {
			if domain.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		return task.Status() == domain.TaskCompleted && incident.Diagnosis() != "", nil
	}
	return false, nil
}

func (uc MarkDiagnosis) refuse(ctx context.Context, id domain.IncidentID, actor string, cause error) error {
	_ = uc.rec.audit(ctx, "autonomy.mark", id.String(), domain.AuditDenied, map[string]string{"actor": actor, "reason": cause.Error()})
	return cause
}
