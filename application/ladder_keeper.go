package application

import (
	"context"
	"strconv"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ladderKeeper is the runbook's ladder as the incident's phases see it:
// what it says of an apply about to launch or about to run, and the
// signals of a cycle (axlr-sre design §8.3). A store that fails is
// audited and the incident goes on. An apply that writes is recorded
// outstanding first, so a lowering that gets lost costs the runbook's
// next apply its card, not a count.
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

// unitGrant is the live unit as what a task pins.
func unitGrant(unit domain.LadderUnit) domain.LadderGrant {
	return domain.LadderGrant{Runbook: unit.Runbook().Name(), Tool: unit.Runbook().Tool(), Namespace: unit.Namespace()}
}

// recordOf reads the unit's record, or starts one at the step a ladder
// starts from; false when it was not stored yet. Any failure to read is
// an error: a fresh record in its place would forget a lowering.
func (k ladderKeeper) recordOf(ctx context.Context, tenant domain.TenantID, bot domain.BotID, unit domain.LadderGrant) (domain.RunbookAutonomy, bool, error) {
	autonomy, err := k.deps.Autonomy.Get(ctx, domain.AutonomyIDOf(tenant, bot, unit.Runbook, unit.Namespace))
	if err == nil {
		return autonomy, true, nil
	}
	if !domain.IsNotFound(err) {
		return domain.RunbookAutonomy{}, false, err
	}
	autonomy, err = domain.NewRunbookAutonomy(tenant, bot, unit.Runbook, unit.Namespace, domain.AutonomyActWithApproval, k.deps.Clock.Now())
	if err != nil {
		return domain.RunbookAutonomy{}, false, err
	}
	autonomy.BindTool(unit.Tool, k.deps.Clock.Now())
	return autonomy, false, nil
}

// decide is what the taker's ladder says of the incident's apply now. A
// record that cannot be read keeps the card (fail closed); a record
// earned with another tool starts over here, durably.
func (k ladderKeeper) decide(ctx context.Context, taker domain.Bot, trigger domain.Trigger, incident domain.Incident) domain.LadderDecision {
	var (
		autonomy domain.RunbookAutonomy
		known    bool
	)
	unit, _, unitOK := domain.LadderUnitOf(trigger, incident)
	if unitOK {
		grant := unitGrant(unit)
		found, stored, err := k.recordOf(ctx, taker.TenantID(), taker.ID(), grant)
		if err != nil {
			return domain.LadderDecision{Laddered: true, Reason: "the runbook's record could not be read: " + err.Error(), Unit: &grant}
		}
		if stored && found.BindTool(grant.Tool, k.deps.Clock.Now()) {
			if err := k.deps.Autonomy.Save(ctx, found); err != nil {
				return domain.LadderDecision{Laddered: true, Reason: "the runbook's record could not be saved: " + err.Error(), Unit: &grant}
			}
			_ = k.rec.audit(ctx, "autonomy.changed", found.ID().String(), domain.AuditOK, map[string]string{"tool": grant.Tool.String(), "step": found.Step().String(), "reason": "the runbook's write tool changed: it earns from zero"})
		}
		autonomy, known = found, stored
	}
	return taker.DecideLadder(trigger, incident, autonomy, known)
}

// begin records the apply outstanding, before it can write.
func (k ladderKeeper) begin(ctx context.Context, incident domain.Incident, bot domain.BotID, unit domain.LadderGrant, cycle int) error {
	autonomy, _, err := k.recordOf(ctx, incident.TenantID(), bot, unit)
	if err != nil {
		return err
	}
	autonomy.Begin(incident.ID(), cycle, k.deps.Clock.Now())
	return k.deps.Autonomy.Save(ctx, autonomy)
}

// abandon drops the apply's outstanding entry: it wrote nothing. A
// failure leaves the entry, and the runbook on the card.
func (k ladderKeeper) abandon(ctx context.Context, incident domain.Incident, bot domain.BotID, unit domain.LadderGrant, cycle int) {
	autonomy, stored, err := k.recordOf(ctx, incident.TenantID(), bot, unit)
	if err == nil && stored && autonomy.Abandon(incident.ID(), cycle, k.deps.Clock.Now()) {
		err = k.deps.Autonomy.Save(ctx, autonomy)
	}
	if err != nil {
		_ = k.rec.audit(ctx, "autonomy.signal", incident.ID().String(), domain.AuditDenied, map[string]string{"signal": "abandon", "cycle": strconv.Itoa(cycle), "reason": err.Error()})
	}
}

// applyTaskOf is the apply task of the incident's cycle and the unit
// pinned on it: where the cycle's signals fall, whatever the incident
// says now. False when the cycle's apply carries no unit.
func (k ladderKeeper) applyTaskOf(ctx context.Context, incident domain.Incident, cycle int) (domain.Task, domain.LadderGrant, bool, error) {
	for _, attached := range incident.Tasks() {
		if attached.Phase() != domain.IncidentPhaseApply || attached.Cycle() != cycle {
			continue
		}
		task, err := k.deps.Tasks.Get(ctx, attached.TaskID())
		if err != nil {
			return domain.Task{}, domain.LadderGrant{}, false, err
		}
		unit, ok := pinnedUnit(task)
		return task, unit, ok, nil
	}
	return domain.Task{}, domain.LadderGrant{}, false, nil
}

func pinnedUnit(task domain.Task) (domain.LadderGrant, bool) {
	pinned, ok := task.Pinned()
	if !ok {
		return domain.LadderGrant{}, false
	}
	return pinned.LadderUnit()
}

// signal records one signal of the incident's cycle on the unit pinned on
// that cycle's apply task. Idempotent per incident, cycle and signal; a
// failure is audited and carried on (the outstanding entry stays).
func (k ladderKeeper) signal(ctx context.Context, incident domain.Incident, cycle int, signal domain.AutonomySignal, actor string) {
	if err := k.record(ctx, incident, cycle, signal, actor); err != nil {
		_ = k.rec.audit(ctx, "autonomy.signal", incident.ID().String(), domain.AuditDenied, map[string]string{"signal": signal.String(), "cycle": strconv.Itoa(cycle), "reason": err.Error()})
	}
}

func (k ladderKeeper) record(ctx context.Context, incident domain.Incident, cycle int, signal domain.AutonomySignal, actor string) error {
	task, unit, ok, err := k.applyTaskOf(ctx, incident, cycle)
	if err != nil || !ok {
		return err
	}
	_, err = k.apply(ctx, incident.TenantID(), task.BotID(), unit, k.thresholdsOf(ctx, task.BotID()), incident, cycle, signal, actor)
	return err
}

// thresholdsOf is what the bot's ladder asks for now, the defaults once
// it has none.
func (k ladderKeeper) thresholdsOf(ctx context.Context, id domain.BotID) domain.AutonomyThresholds {
	bot, err := k.deps.Bots.Get(ctx, id)
	if err != nil {
		return domain.DefaultAutonomyThresholds()
	}
	if trigger, ok := bot.Triggers().Incident(domain.IncidentPhaseApply); ok {
		if ladder, ok := trigger.Ladder(); ok {
			return ladder.Thresholds()
		}
	}
	return domain.DefaultAutonomyThresholds()
}

// apply records the signal on the unit's record and saves it, audited
// with the step it moved, when it moved one. A record since bound to
// another tool takes a lowering, and counts nothing of the old tool's.
func (k ladderKeeper) apply(ctx context.Context, tenant domain.TenantID, bot domain.BotID, unit domain.LadderGrant, thresholds domain.AutonomyThresholds, incident domain.Incident, cycle int, signal domain.AutonomySignal, actor string) (domain.RunbookAutonomy, error) {
	autonomy, _, err := k.recordOf(ctx, tenant, bot, unit)
	if err != nil {
		return domain.RunbookAutonomy{}, err
	}
	if autonomy.Tool() == "" {
		autonomy.BindTool(unit.Tool, k.deps.Clock.Now())
	}
	if autonomy.Tool() != unit.Tool && !signal.Lowers() {
		return autonomy, nil
	}
	change, err := autonomy.Record(signal, incident.ID(), cycle, actor, thresholds, k.deps.Clock.Now())
	if err != nil {
		return autonomy, err
	}
	if !change.Recorded {
		return autonomy, nil
	}
	if err := k.deps.Autonomy.Save(ctx, autonomy); err != nil {
		return autonomy, err
	}
	details := map[string]string{"incident": incident.ID().String(), "cycle": strconv.Itoa(cycle), "signal": signal.String(), "runbook": unit.Runbook.String(), "namespace": unit.Namespace, "step": autonomy.Step().String()}
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
	autonomy, saved, err := k.recordOf(ctx, taker.TenantID(), taker.ID(), unitGrant(unit))
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

// purge removes every record of the bot: a bot registered again under
// its id earns from zero.
func (k ladderKeeper) purge(ctx context.Context, bot domain.BotID) error {
	records, err := k.deps.Autonomy.List(ctx)
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.BotID() != bot {
			continue
		}
		if err := k.deps.Autonomy.Delete(ctx, record.ID()); err != nil {
			return err
		}
		if err := k.rec.audit(ctx, "autonomy.deleted", record.ID().String(), domain.AuditOK, map[string]string{"bot": bot.String(), "runbook": record.Runbook().String(), "namespace": record.Namespace()}); err != nil {
			return err
		}
	}
	return nil
}
