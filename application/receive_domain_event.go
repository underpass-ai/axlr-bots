package application

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ReceiveDomainEvent is the listening side of a domain event between
// teams (docs/architecture/team-topics.md): the bus hands it the event
// another team published, and for each team of the audience in the
// event's tenant it makes the task of the bots that hear it, through a
// subscription that names the publishing team. A deliver: team
// subscription makes one task for the team, others one per hearer. The
// task starts a new chain (no lineage, depth 0) and keeps where it came
// from (its origin); it is never joined to a Pending task as a note,
// never degraded, and it is made even while its bot is Paused, so the
// fact waits for it rather than being lost. The same fact received
// twice makes one task, by its key. A team where nobody hears it is a
// skip team_unavailable.
type ReceiveDomainEvent struct {
	deps Dependencies
	rec  recorder
}

// NewReceiveDomainEvent wires the use case.
func NewReceiveDomainEvent(deps Dependencies) ReceiveDomainEvent {
	return ReceiveDomainEvent{deps: deps, rec: recorder{deps: deps}}
}

// crossHearer is a bot of an audience team that hears the event, and the
// subscription it hears it by.
type crossHearer struct {
	bot domain.Bot
	sub domain.Subscription
}

// Execute delivers the event to every team of its audience. An error is
// one the bus should give the event again for; a skip is recorded and is
// none.
func (uc ReceiveDomainEvent) Execute(ctx context.Context, event CrossingEvent) (ReceiveAnswer, error) {
	all, err := uc.deps.Bots.List(ctx)
	if err != nil {
		return ReceiveAnswer{}, err
	}
	slices.SortFunc(all, func(a, b domain.Bot) int { return strings.Compare(a.ID().String(), b.ID().String()) })
	var answer ReceiveAnswer
	add := func(delivery *EventDelivery, skip *EventSkip) error {
		if delivery != nil {
			answer.Deliveries = append(answer.Deliveries, *delivery)
		}
		if skip != nil {
			answer.Skipped = append(answer.Skipped, *skip)
			return uc.skip(ctx, event, *skip)
		}
		return nil
	}
	for _, team := range event.Audience.Teams() {
		together, alone := uc.hearers(event, team, all)
		if len(together) == 0 && len(alone) == 0 {
			skip := EventSkip{Reason: domain.RefusedTeamUnavailable, Team: team.String(), Crossed: true,
				Message: "no bot of team " + team.String() + " in tenant " + event.Tenant.String() + " hears " + event.Event.Name().String() + " from team " + event.Event.Team().String()}
			if err := add(nil, &skip); err != nil {
				return ReceiveAnswer{}, err
			}
			continue
		}
		for _, h := range alone {
			key, err := domain.NewIdempotencyKey("event:" + event.Event.Identity() + ":" + h.bot.ID().String())
			if err != nil {
				return ReceiveAnswer{}, err
			}
			delivery, skip, err := uc.deliverTask(ctx, event, h, key, team, false)
			if err != nil {
				return ReceiveAnswer{}, err
			}
			if err := add(delivery, skip); err != nil {
				return ReceiveAnswer{}, err
			}
		}
		if len(together) > 0 {
			delivery, skip, err := uc.deliverTeam(ctx, event, team, together)
			if err != nil {
				return ReceiveAnswer{}, err
			}
			if err := add(delivery, skip); err != nil {
				return ReceiveAnswer{}, err
			}
		}
	}
	return answer, nil
}

// hearers are the bots of the audience team, in the event's tenant, that
// hear the event through a subscription naming the publishing team: those
// of a deliver: team subscription, who take it together, and the others.
func (uc ReceiveDomainEvent) hearers(event CrossingEvent, team domain.TeamName, all []domain.Bot) (together, alone []crossHearer) {
	for _, bot := range all {
		if bot.TenantID() != event.Tenant || memoryTeamOf(bot) != team {
			continue
		}
		sub, hears := bot.Subscriptions().HearingFrom(event.Event.Team(), event.Event.Name(), event.Event.From().String())
		if !hears {
			continue
		}
		if sub.Deliver().Team() {
			together = append(together, crossHearer{bot: bot, sub: sub})
		} else {
			alone = append(alone, crossHearer{bot: bot, sub: sub})
		}
	}
	return together, alone
}

// deliverTeam makes the team's one task: the one the same fact made
// already, found among every hearer, else a task of the member picked
// among those that take tasks now, else, when every hearer is Paused or
// not yet enabled, of the member picked among them all: the task waits
// Pending until its bot is enabled.
func (uc ReceiveDomainEvent) deliverTeam(ctx context.Context, event CrossingEvent, team domain.TeamName, hearers []crossHearer) (*EventDelivery, *EventSkip, error) {
	key, err := domain.NewIdempotencyKey("event:" + event.Event.Identity() + ":team:" + team.String())
	if err != nil {
		return nil, nil, err
	}
	for _, h := range hearers {
		task, found, err := uc.deps.Tasks.FindByIdempotencyKey(ctx, h.bot.ID(), key, h.bot.CreatedAt())
		if err != nil {
			return nil, nil, err
		}
		if found {
			// The same fact again: the task it made, not a second one.
			return &EventDelivery{BotID: h.bot.ID().String(), Mode: domain.ModeTask.String(), TaskID: task.ID().String(), Trigger: h.sub.TriggerName(), Team: team.String(), Crossed: true}, nil, nil
		}
	}
	var pool []domain.Bot
	for _, h := range hearers {
		if h.bot.AcceptsTasks() {
			pool = append(pool, h.bot)
		}
	}
	if len(pool) == 0 {
		for _, h := range hearers {
			pool = append(pool, h.bot)
		}
	}
	picked, err := pickMember(ctx, uc.deps, pool)
	if err != nil {
		return nil, nil, err
	}
	h := hearers[slices.IndexFunc(hearers, func(h crossHearer) bool { return h.bot.ID() == picked.ID() })]
	return uc.deliverTask(ctx, event, h, key, team, true)
}

// deliverTask makes the hearer's task of the event under the key, or
// finds the one the key made; together says the team takes it. An
// objective that does not render is a skip: giving the event again would
// not mend the subscription.
func (uc ReceiveDomainEvent) deliverTask(ctx context.Context, event CrossingEvent, h crossHearer, key domain.IdempotencyKey, team domain.TeamName, together bool) (*EventDelivery, *EventSkip, error) {
	fields := crossedFields(event)
	objective, err := crossedObjective(h.sub, fields)
	if err != nil {
		return nil, &EventSkip{BotID: h.bot.ID().String(), Reason: "objective", Team: team.String(), Crossed: true, Message: "the objective of " + h.bot.ID().String() + "'s subscription " + h.sub.TriggerName() + " does not render: " + err.Error()}, nil
	}
	trigger, err := domain.NewTriggerRef(domain.TriggerEvent.String(), h.sub.TriggerName())
	if err != nil {
		return nil, nil, err
	}
	input, err := json.Marshal(fields)
	if err != nil {
		return nil, nil, err
	}
	// The task carries the emitter's trace, so the whole way across the
	// teams reads as one; its chain is its team's own.
	correlation, err := domain.NewCorrelationID(event.TraceID)
	if err != nil {
		correlation = "" // a new trace, then
	}
	origin := event.Origin()
	created, err := submitTask(ctx, uc.deps, submission{
		action: "task.event", bot: h.bot, trigger: trigger, objective: objective, key: key,
		correlation: correlation, input: input, origin: &origin, waits: true,
	})
	if err != nil {
		return nil, nil, err
	}
	delivery := EventDelivery{BotID: h.bot.ID().String(), Mode: domain.ModeTask.String(), TaskID: created.Task.ID().String(), Trigger: h.sub.TriggerName(), Created: created.Created, Team: team.String(), Crossed: true}
	if created.Created {
		if err := uc.recordHeard(ctx, event, h.bot, created.Task, delivery, together); err != nil {
			return nil, nil, err
		}
	}
	return &delivery, nil, nil
}

// crossedFields are what the subscription's objective template reads and
// what the task keeps as its input: the keys of a teammate's event, with
// no message (a domain event carries none), and the fact's own.
func crossedFields(event CrossingEvent) map[string]any {
	data := map[string]any{"id": event.Event.ID().String()}
	if event.Event.Context() != "" {
		data["context"] = event.Event.Context().String()
	}
	dataJSON, _ := json.Marshal(data)
	return map[string]any{
		"eventId": event.Event.Identity(), "name": event.Event.Name().String(), "from": event.Event.From().String(),
		"team": event.Event.Team().String(), "id": event.Event.ID().String(), "context": event.Event.Context().String(),
		"severity": domain.SeverityInfo.String(), "message": "", "data": data, "dataJSON": string(dataJSON),
		"to": []string{}, "teams": []string{event.Event.Team().String()}, "taskId": "", "crossings": event.Crossings,
	}
}

// crossedObjective renders the subscription's template (or the default
// for another team's event) and adds the event under its heading, as
// data: the receiver reads it as another team's facts, never as orders.
func crossedObjective(sub domain.Subscription, fields map[string]any) (domain.Objective, error) {
	template := sub.ObjectiveTemplate()
	if template == "" {
		template = domain.DefaultCrossedEventObjective
	}
	rendered, err := domain.RenderObjective(template, fields)
	if err != nil {
		return "", err
	}
	facts := map[string]any{"team": fields["team"], "from": fields["from"], "name": fields["name"], "data": fields["data"]}
	raw, err := json.Marshal(facts)
	if err != nil {
		return "", err
	}
	return domain.NewObjective(rendered.String() + "\n\n" + domain.CrossedEventHeading + "\n" + string(raw))
}

// recordHeard records on the receiver's stream that it heard the other
// team's event, and audits the crossing.
func (uc ReceiveDomainEvent) recordHeard(ctx context.Context, event CrossingEvent, bot domain.Bot, task domain.Task, d EventDelivery, together bool) error {
	payload := map[string]any{
		"eventId": event.Event.Identity(), "name": event.Event.Name().String(), "from": event.Event.From().String(), "by": string(domain.PublisherBot),
		"fromTeam": event.Event.Team().String(), "severity": domain.SeverityInfo.String(), "message": "", "mode": d.Mode, "subscription": d.Trigger,
		"taskId": d.TaskID, "urgent": false, "crossed": true, "crossings": event.Crossings,
	}
	if together {
		payload["team"] = d.Team
	}
	if !bot.AcceptsTasks() {
		payload["waits"] = bot.Status().String()
	}
	if err := uc.rec.event(ctx, domain.EventEventHeard, bot, task.ID(), task.CorrelationID(), payload); err != nil {
		return err
	}
	return uc.rec.audit(ctx, "event.cross", task.ID().String(), domain.AuditOK, map[string]string{
		"event": event.Event.Identity(), "name": event.Event.Name().String(), "from": event.Event.From().String(), "fromTeam": event.Event.Team().String(),
		"team": d.Team, "bot": bot.ID().String(), "crossings": strconv.Itoa(event.Crossings),
	})
}

// skip audits a team or bot the event did not reach.
func (uc ReceiveDomainEvent) skip(ctx context.Context, event CrossingEvent, s EventSkip) error {
	subject := s.Team
	if s.BotID != "" {
		subject = s.BotID
	}
	return uc.rec.audit(ctx, "event.cross", subject, domain.AuditDenied, map[string]string{
		"event": event.Event.Identity(), "name": event.Event.Name().String(), "from": event.Event.From().String(), "fromTeam": event.Event.Team().String(),
		"team": s.Team, "reason": s.Reason, "crossings": strconv.Itoa(event.Crossings),
	})
}
