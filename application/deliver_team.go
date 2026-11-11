package application

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// teamHearer is a teammate that hears an event through a team
// subscription.
type teamHearer struct {
	bot domain.Bot
	sub domain.Subscription
}

// deliverAll gives the event to the teammates: each in its own way, as
// deliver says, except the hearers of a team subscription, whose team
// gets one task between them (docs/architecture/team-subscriptions.md).
// The deliveries and skips come in the teammates' order, the teams'
// after them.
func (uc EmitEvent) deliverAll(ctx context.Context, e emission, mates []domain.Bot) ([]EventDelivery, []EventSkip, error) {
	var deliveries []EventDelivery
	var skips []EventSkip
	add := func(delivery *EventDelivery, skip *EventSkip) {
		if delivery != nil {
			deliveries = append(deliveries, *delivery)
		}
		if skip != nil {
			skips = append(skips, *skip)
		}
	}
	teams := map[domain.TeamName][]teamHearer{}
	var order []domain.TeamName
	for _, mate := range mates {
		if sub, ok := e.teamHearing(mate); ok {
			team := memoryTeamOf(mate)
			if _, seen := teams[team]; !seen {
				order = append(order, team)
			}
			teams[team] = append(teams[team], teamHearer{bot: mate, sub: sub})
			continue
		}
		delivery, skip, err := uc.deliver(ctx, e, mate)
		if err != nil {
			return nil, nil, err
		}
		add(delivery, skip)
	}
	for _, team := range order {
		delivery, skip, err := uc.deliverTeam(ctx, e, team, teams[team])
		if err != nil {
			return nil, nil, err
		}
		add(delivery, skip)
	}
	return deliveries, skips, nil
}

// teamHearing reports whether the teammate hears the event through a
// team subscription: never an addressed event, nor a critical one, which
// reach every hearer as before.
func (e emission) teamHearing(mate domain.Bot) (domain.Subscription, bool) {
	if len(e.to) > 0 || e.severity == domain.SeverityCritical {
		return domain.Subscription{}, false
	}
	sub, hears := mate.Hears(e.name, e.from, e.by, false)
	if !hears || sub.Implicit() || !sub.Deliver().Team() {
		return domain.Subscription{}, false
	}
	return sub, true
}

// deliverTeam gives the event to one hearer of the team: the task the
// same event made already, a Pending event task of the same name it
// joins as a note, or a new task of the member picked (the first free
// one by id; among those an Online node may run, when there are any).
func (uc EmitEvent) deliverTeam(ctx context.Context, e emission, team domain.TeamName, hearers []teamHearer) (*EventDelivery, *EventSkip, error) {
	slices.SortFunc(hearers, func(a, b teamHearer) int { return strings.Compare(a.bot.ID().String(), b.bot.ID().String()) })
	var candidates []teamHearer
	for _, hearer := range hearers {
		if hearer.bot.AcceptsTasks() {
			candidates = append(candidates, hearer)
		}
	}
	if len(candidates) == 0 {
		return nil, &EventSkip{Reason: domain.RefusedTeamUnavailable, Team: team.String(), Message: "no bot of team " + team.String() + " that hears " + e.name.String() + " takes tasks"}, nil
	}
	key, err := eventTaskKey(e, domain.Bot{}, team)
	if err != nil {
		return nil, nil, err
	}
	for _, c := range candidates {
		task, found, err := uc.deps.Tasks.FindByIdempotencyKey(ctx, c.bot.ID(), key, time.Time{})
		if err != nil {
			return nil, nil, err
		}
		if found {
			// The same event again, after a crash between its task and
			// its record: the task it made, not a second one.
			return &EventDelivery{BotID: c.bot.ID().String(), Mode: domain.ModeTask.String(), TaskID: task.ID().String(), Trigger: c.sub.TriggerName(), Urgent: task.IsUrgent(), Team: team.String()}, nil, nil
		}
	}
	for _, c := range candidates {
		pending, found, err := pendingTaskOf(ctx, uc.deps, c.bot, e.name)
		if err != nil {
			return nil, nil, err
		}
		if found {
			return uc.deliverNote(ctx, e, c.bot, c.sub, "", pending.ID().String(), team)
		}
	}
	pool, err := uc.reachable(ctx, candidates)
	if err != nil {
		return nil, nil, err
	}
	load, err := readMemberLoad(ctx, uc.deps, nil)
	if err != nil {
		return nil, nil, err
	}
	bots := make([]domain.Bot, len(pool))
	for i, c := range pool {
		bots[i] = c.bot
	}
	picked, _ := load.pick(bots)
	hearer := pool[slices.IndexFunc(pool, func(c teamHearer) bool { return c.bot.ID() == picked.ID() })]
	urgent := hearer.bot.InterruptedBy(hearer.sub, domain.KindOf(e.name))
	return uc.deliverTask(ctx, e, hearer.bot, hearer.sub, urgent, team)
}

// reachable keeps the candidates some Online node may run; all of them
// when none is, so the task waits Pending for a node as a task does.
func (uc EmitEvent) reachable(ctx context.Context, candidates []teamHearer) ([]teamHearer, error) {
	nodes, err := uc.deps.Nodes.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []teamHearer
	for _, c := range candidates {
		if slices.ContainsFunc(nodes, func(node domain.Node) bool { return nodeCanRun(c.bot, domain.SkillPins{}, node) }) {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return candidates, nil
	}
	return out, nil
}

// eventTaskKey is the key of the task an event makes: one per receiver,
// or one per team for a team subscription.
func eventTaskKey(e emission, mate domain.Bot, team domain.TeamName) (domain.IdempotencyKey, error) {
	if team != "" {
		return domain.NewIdempotencyKey("event:" + e.id + ":team:" + team.String())
	}
	return domain.NewIdempotencyKey("event:" + e.id + ":" + mate.ID().String())
}
