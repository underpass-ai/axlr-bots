package application

import (
	"context"
	"fmt"

	"github.com/underpass-ai/axlr-bots/domain"
)

// cross publishes the event to the other teams the bot's emit rule names
// for it (docs/architecture/team-topics.md), once, through the
// domain-event bus: their tasks are made when the bus hands it to
// ReceiveDomainEvent, so the deliveries name the teams, not a bot or a
// task. Nothing crosses for a name no rule sends elsewhere, nor for an
// addressed event. An event that would cross but cannot is a skip per
// team: no data.id (or a data.context that is no KMP key), the trace's
// crossings spent, or no bus.
func (uc EmitEvent) cross(ctx context.Context, e emission) ([]EventDelivery, []EventSkip, error) {
	teams := e.bot.Events().CrossesTo(e.name)
	if len(teams) == 0 || len(e.to) > 0 {
		return nil, nil, nil
	}
	skipAll := func(reason, message string) ([]EventDelivery, []EventSkip, error) {
		skips := make([]EventSkip, len(teams))
		for i, team := range teams {
			skips[i] = EventSkip{Reason: reason, Team: team.String(), Crossed: true, Message: message}
		}
		return nil, skips, nil
	}
	raw, _ := e.data["id"].(string)
	id, err := domain.NewDomainID(raw)
	if err != nil {
		return skipAll(domain.RefusedNoDomainID, fmt.Sprintf("%s crosses to other teams only with the thing's id in data.id (1 to 128 letters, digits, '.', '_', ':', '@' or '-'): it did not cross", e.name))
	}
	var key domain.ContextKey
	if value, present := e.data["context"]; present {
		text, _ := value.(string)
		if key, err = domain.NewContextKey(text); err != nil {
			return skipAll(domain.RefusedNoDomainID, fmt.Sprintf("data.context of %s is no KMP key <kind>:<name> without spaces: it did not cross", e.name))
		}
	}
	before, err := traceOrigin(ctx, uc.deps, e.task)
	if err != nil {
		return nil, nil, err
	}
	crossings, ok := domain.NextCrossings(before)
	if !ok {
		return skipAll(domain.RefusedCrossingLimit, fmt.Sprintf("this trace crossed %d team boundaries already, its most: %s did not cross", domain.MaxTeamCrossings, e.name))
	}
	if uc.deps.DomainEvents == nil {
		return skipAll(domain.RefusedTeamUnavailable, "this control plane carries no domain events between teams: "+e.name.String()+" did not cross")
	}
	team := memoryTeamOf(e.bot)
	event, err := domain.NewDomainEvent(e.name, id, key, team, e.bot.ID(), uc.deps.Clock.Now())
	if err != nil {
		return nil, nil, err
	}
	audience, err := domain.NewAudience(team, teams)
	if err != nil {
		return nil, nil, err
	}
	crossing := CrossingEvent{Tenant: e.bot.TenantID(), Event: event, Audience: audience, TraceID: e.task.CorrelationID().String(), Crossings: crossings}
	if err := uc.deps.DomainEvents.Publish(ctx, crossing); err != nil {
		return nil, nil, err
	}
	deliveries := make([]EventDelivery, len(teams))
	for i, to := range teams {
		deliveries[i] = EventDelivery{Mode: domain.ModeTask.String(), Trigger: e.name.String(), Created: true, Team: to.String(), Crossed: true}
	}
	return deliveries, nil, nil
}

// crossedCount is how many of the deliveries went to other teams.
func crossedCount(deliveries []EventDelivery) int {
	n := 0
	for _, d := range deliveries {
		if d.Crossed {
			n++
		}
	}
	return n
}
