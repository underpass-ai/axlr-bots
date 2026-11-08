package application

import (
	"context"
	"strconv"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ladderKeeper is the runbook's ladder as the incident's phases see it:
// what it says of an apply about to launch, and the signals of a cycle
// (axlr-sre design §8.3). A store that fails is audited and the incident
// goes on: a lost signal costs a count, never an apply.
type ladderKeeper struct {
	deps Dependencies
	rec  recorder
}

func newLadderKeeper(deps Dependencies) ladderKeeper {
	return ladderKeeper{deps: deps, rec: recorder{deps: deps}}
}

// takerOf is the one teammate of the incident's triage bot taking the
// apply phase, with its trigger; false when there is none, or two (the
// launcher leaves those alone too).
func (k ladderKeeper) takerOf(ctx context.Context, incident domain.Incident) (domain.Bot, domain.Trigger, bool, error) {
	triage, err := k.deps.Bots.Get(ctx, incident.BotID())
	if domain.IsNotFound(err) {
		return domain.Bot{}, domain.Trigger{}, false, nil
	}
	if err != nil {
		return domain.Bot{}, domain.Trigger{}, false, err
	}
	mates, err := teammatesOf(ctx, k.deps, triage)
	if err != nil {
		return domain.Bot{}, domain.Trigger{}, false, err
	}
	var (
		taker   domain.Bot
		trigger domain.Trigger
		found   int
	)
	for _, mate := range mates {
		if own, ok := mate.Triggers().Incident(domain.IncidentPhaseApply); ok {
			taker, trigger = mate, own
			found++
		}
	}
	return taker, trigger, found == 1, nil
}

// load reads the unit's record, or starts one at the step the trigger
// declares; false when it was not stored yet.
func (k ladderKeeper) load(ctx context.Context, taker domain.Bot, trigger domain.Trigger, unit domain.LadderUnit) (domain.RunbookAutonomy, bool, error) {
	autonomy, err := k.deps.Autonomy.Get(ctx, unit.AutonomyID(taker.TenantID(), taker.ID()))
	if err == nil {
		return autonomy, true, nil
	}
	if !domain.IsNotFound(err) {
		return domain.RunbookAutonomy{}, false, err
	}
	level, _ := trigger.Level()
	autonomy, err = domain.NewRunbookAutonomy(taker.TenantID(), taker.ID(), unit.Runbook().Name(), unit.Namespace(), level, k.deps.Clock.Now())
	return autonomy, false, err
}

// decide is what the taker's ladder says of the incident's apply now. A
// record that cannot be read keeps the card (fail closed).
func (k ladderKeeper) decide(ctx context.Context, taker domain.Bot, trigger domain.Trigger, incident domain.Incident) domain.LadderDecision {
	var (
		autonomy domain.RunbookAutonomy
		known    bool
	)
	if unit, _, ok := domain.LadderUnitOf(trigger, incident); ok {
		found, stored, err := k.load(ctx, taker, trigger, unit)
		if err != nil {
			return domain.LadderDecision{Laddered: true, Reason: "the runbook's record could not be read: " + err.Error()}
		}
		autonomy, known = found, stored
	}
	return taker.DecideLadder(trigger, incident, autonomy, known)
}

// signal records one signal of the incident's cycle on its runbook, if
// the incident is on a ladder. Idempotent per incident, cycle and
// signal; a failure is audited and carried on.
func (k ladderKeeper) signal(ctx context.Context, incident domain.Incident, cycle int, signal domain.AutonomySignal, actor string) {
	if err := k.record(ctx, incident, cycle, signal, actor); err != nil {
		_ = k.rec.audit(ctx, "autonomy.signal", incident.ID().String(), domain.AuditDenied, map[string]string{"signal": signal.String(), "cycle": strconv.Itoa(cycle), "reason": err.Error()})
	}
}

func (k ladderKeeper) record(ctx context.Context, incident domain.Incident, cycle int, signal domain.AutonomySignal, actor string) error {
	taker, trigger, ok, err := k.takerOf(ctx, incident)
	if err != nil || !ok {
		return err
	}
	unit, _, ok := domain.LadderUnitOf(trigger, incident)
	if !ok {
		return nil
	}
	_, err = k.apply(ctx, taker, trigger, unit, incident, cycle, signal, actor)
	return err
}

// apply records the signal on the unit's record and saves it, audited
// with the step it moved, when it moved one.
func (k ladderKeeper) apply(ctx context.Context, taker domain.Bot, trigger domain.Trigger, unit domain.LadderUnit, incident domain.Incident, cycle int, signal domain.AutonomySignal, actor string) (domain.RunbookAutonomy, error) {
	autonomy, _, err := k.load(ctx, taker, trigger, unit)
	if err != nil {
		return domain.RunbookAutonomy{}, err
	}
	change, err := autonomy.Record(signal, incident.ID(), cycle, actor, unit.Thresholds(), k.deps.Clock.Now())
	if err != nil {
		return autonomy, err
	}
	if !change.Recorded {
		return autonomy, nil
	}
	if err := k.deps.Autonomy.Save(ctx, autonomy); err != nil {
		return autonomy, err
	}
	details := map[string]string{"incident": incident.ID().String(), "cycle": strconv.Itoa(cycle), "signal": signal.String(), "runbook": unit.Runbook().Name().String(), "namespace": unit.Namespace(), "step": autonomy.Step().String()}
	if actor != "" {
		details["actor"] = actor
	}
	action := "autonomy.signal"
	if change.Moved() {
		action = "autonomy.changed"
		details["from"], details["to"] = change.From.String(), change.To.String()
	}
	return autonomy, k.rec.audit(ctx, action, autonomy.ID().String(), domain.AuditOK, details)
}

// view is the standing the incident's unit shows, nil when the incident
// is on no ladder.
func (k ladderKeeper) view(ctx context.Context, incident domain.Incident) (*AutonomyView, error) {
	taker, trigger, ok, err := k.takerOf(ctx, incident)
	if err != nil || !ok {
		return nil, err
	}
	unit, _, ok := domain.LadderUnitOf(trigger, incident)
	if !ok {
		return nil, nil
	}
	autonomy, saved, err := k.load(ctx, taker, trigger, unit)
	if err != nil {
		return nil, err
	}
	thresholds := unit.Thresholds().Snapshot()
	view := AutonomyView{Autonomy: autonomy, Thresholds: &thresholds, Saved: saved}
	if mark, marked := autonomy.DiagnosisMark(incident.ID(), incident.Cycle()); marked {
		view.DiagnosisMark = mark.String()
	}
	return &view, nil
}
