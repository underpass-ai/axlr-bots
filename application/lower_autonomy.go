package application

import (
	"context"
	"strconv"

	"github.com/underpass-ai/axlr-bots/domain"
)

// LowerAutonomy is a person stopping a runbook: one step down by hand,
// whatever it earned, every outstanding apply ended, and any run of it
// already acting alone cancelled. A task granted that has not started
// is taken back when it comes to run.
type LowerAutonomy struct {
	deps Dependencies
	rec  recorder
}

// NewLowerAutonomy wires the use case.
func NewLowerAutonomy(deps Dependencies) LowerAutonomy {
	return LowerAutonomy{deps: deps, rec: recorder{deps: deps}}
}

// Execute lowers the runbook and answers its standing. A record Get
// cannot read is the error it gives: nothing is changed.
func (uc LowerAutonomy) Execute(ctx context.Context, id domain.AutonomyID, actor, reason string) (AutonomyView, error) {
	autonomy, err := uc.deps.Autonomy.Get(ctx, id)
	if err != nil {
		return AutonomyView{}, err
	}
	name, err := autonomyActor(ctx, uc.deps, autonomy, actor)
	if err != nil {
		return AutonomyView{}, refuseAutonomy(ctx, uc.rec, "autonomy.lower", id, actor, err)
	}
	from := autonomy.Step()
	if err := autonomy.Lower(name, reason, uc.deps.Clock.Now()); err != nil {
		return AutonomyView{}, refuseAutonomy(ctx, uc.rec, "autonomy.lower", id, actor, err)
	}
	if err := uc.deps.Autonomy.Save(ctx, autonomy); err != nil {
		return AutonomyView{}, err
	}
	cancelled, err := uc.stopRuns(ctx, autonomy, name)
	if err != nil {
		return AutonomyView{}, err
	}
	details := map[string]string{"actor": name, "from": from.String(), "to": autonomy.Step().String(), "signal": domain.SignalLoweredByPerson.String(), "cancelled": strconv.Itoa(cancelled)}
	if history := autonomy.History(); history[len(history)-1].Reason != "" {
		details["reason"] = history[len(history)-1].Reason
	}
	if err := uc.rec.audit(ctx, "autonomy.changed", id.String(), domain.AuditOK, details); err != nil {
		return AutonomyView{}, err
	}
	return viewOf(ctx, uc.deps, autonomy)
}

// stopRuns cancels the unit's runs already acting alone. A run assigned
// to a node that never leased it has not started: the recheck takes it
// back when it is delivered.
func (uc LowerAutonomy) stopRuns(ctx context.Context, autonomy domain.RunbookAutonomy, name string) (int, error) {
	running, err := uc.deps.Tasks.ListByStatus(ctx, domain.TaskRunning)
	if err != nil {
		return 0, err
	}
	cancelled := 0
	for _, task := range running {
		unit, ok := pinnedUnit(task)
		if !ok || !actedAlone(task) || task.BotID() != autonomy.BotID() || unit.Runbook != autonomy.Runbook() || unit.Namespace != autonomy.Namespace() {
			continue
		}
		execution, err := uc.deps.Executions.Get(ctx, task.CurrentExecutionID())
		if err != nil {
			return cancelled, err
		}
		if execution.IsAssigned() && execution.LeaseUntil().IsZero() {
			continue
		}
		bot, err := uc.deps.Bots.Get(ctx, task.BotID())
		if err != nil {
			return cancelled, err
		}
		if _, err := stopTask(ctx, uc.deps, bot, task, "runbook "+autonomy.Runbook().String()+" lowered by "+name, domain.EventTaskCancelled, "task.cancel", nil); err != nil {
			return cancelled, err
		}
		cancelled++
	}
	return cancelled, nil
}
