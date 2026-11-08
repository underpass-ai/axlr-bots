package application

import (
	"context"
	"strconv"

	"github.com/underpass-ai/axlr-bots/domain"
)

// recheck is the card a run acting alone does not have: immediately
// before an unattended run starts, the ladder is asked again and the
// incident must still stand on the task. Not standing, the task is
// cancelled and nothing runs (false). Standing but no longer granted (the
// runbook lowered, the trigger or the bounds edited, a namespace or two
// persons since, a record that cannot be read), the grant is taken back
// and the run takes the card. Either way it is audited. Any task that
// holds no grant passes untouched.
func (k ladderKeeper) recheck(ctx context.Context, bot domain.Bot, task *domain.Task) (bool, error) {
	pinned, ok := task.Pinned()
	if !ok || !pinned.Unattended() {
		return true, nil
	}
	unit, _ := pinned.LadderUnit()
	stands, incident, err := incidentStanding{deps: k.deps}.standsOn(ctx, *task)
	if err != nil {
		return true, k.takeBack(ctx, bot, task, incident, unit, "its incident could not be read: "+err.Error())
	}
	if incident.ID() == "" {
		return true, k.takeBack(ctx, bot, task, incident, unit, "its incident is gone")
	}
	if !stands {
		return false, k.noRun(ctx, bot, *task, incident, unit)
	}
	trigger, found := bot.Triggers().Named(task.Trigger().Name)
	if !found {
		return true, k.takeBack(ctx, bot, task, incident, unit, "its trigger is gone")
	}
	decision := k.decide(ctx, bot, trigger, incident)
	if decision.Grant == nil {
		return true, k.takeBack(ctx, bot, task, incident, unit, decision.Reason)
	}
	if decision.Grant.Runbook != unit.Runbook || decision.Grant.Tool != unit.Tool || decision.Grant.Namespace != unit.Namespace {
		return true, k.takeBack(ctx, bot, task, incident, unit, "its unit changed since it launched")
	}
	return true, nil
}

// takeBack leaves the task at the card it would have had.
func (k ladderKeeper) takeBack(ctx context.Context, bot domain.Bot, task *domain.Task, incident domain.Incident, unit domain.LadderGrant, reason string) error {
	if !task.RevokeAlone() {
		return nil
	}
	if err := k.deps.Tasks.Save(ctx, *task); err != nil {
		return err
	}
	if incident.ID() != "" {
		k.abandon(ctx, incident, bot.ID(), unit, incident.Cycle())
	}
	return k.rec.audit(ctx, "incident.apply", task.IncidentID().String(), domain.AuditDenied, map[string]string{"task": task.ID().String(), "ladder": "the grant was taken back: " + reason, "level": domain.AutonomyActWithApproval.String()})
}

// noRun cancels a task its incident no longer stands on.
func (k ladderKeeper) noRun(ctx context.Context, bot domain.Bot, task domain.Task, incident domain.Incident, unit domain.LadderGrant) error {
	if cycle, ok := applyCycle(incident, task.ID()); ok {
		k.abandon(ctx, incident, bot.ID(), unit, cycle)
	}
	reason := "its incident no longer stands on it (" + incident.Status().String() + ", cycle " + strconv.Itoa(incident.Cycle()) + ")"
	if _, err := stopTask(ctx, k.deps, bot, task, reason, domain.EventTaskCancelled, "task.cancel", nil); err != nil {
		return err
	}
	return k.rec.audit(ctx, "incident.apply", task.IncidentID().String(), domain.AuditDenied, map[string]string{"task": task.ID().String(), "ladder": "no run: " + reason})
}

func applyCycle(incident domain.Incident, id domain.TaskID) (int, bool) {
	for _, attached := range incident.Tasks() {
		if attached.TaskID() == id {
			return attached.Cycle(), true
		}
	}
	return 0, false
}
