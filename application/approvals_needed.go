package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// approvalsNeeded is how many different people must approve the call
// the task stopped on: two when the bot's approvals.twoPersons names
// the namespace or the severity of the task's incident, else one. Two
// persons is for incidents only (decided 8 November 2026): a task of no
// incident needs one. An incident that cannot be read is taken to need
// two when the bot asks for two persons anywhere (fail closed).
func approvalsNeeded(ctx context.Context, deps Dependencies, bot domain.Bot, task domain.Task) int {
	approvals := bot.Approvals()
	if task.IncidentID() == "" || !approvals.TwoPersons().Set() {
		return 1
	}
	incident, err := deps.Incidents.Get(ctx, task.IncidentID())
	if err != nil {
		return 2
	}
	return approvals.ApprovalsNeeded(incident.Namespace(), incident.Severity())
}
