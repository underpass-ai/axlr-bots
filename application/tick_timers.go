package application

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// TickTimers is the scheduler's pass: for every timer trigger of every
// bot it arms a timer that has none (an interval fires at once, a cron
// at its next slot), re-arms one whose schedule changed, and fires the
// ones that are due, once each: the fire is for the persisted slot, so a
// daemon that was down fires the slot it missed once and arms the next
// one after now rather than replaying every slot in between. A bot that
// takes no tasks has its timers advanced, not fired, and a timer it
// never had is not armed until it takes tasks: a bot registered and then
// enabled fires its interval at once, whether or not a pass fell between
// the two (before, such a pass armed the timer and advanced its first
// slot, so the bot waited a whole interval; QA 5 Nov 2026). Timers of
// triggers that no longer exist are removed.
//
// A timer trigger that says deliver: team is its team's: the members
// that declare it share one timer, and each slot goes to one of them
// (docs/architecture/team-subscriptions.md).
type TickTimers struct {
	deps Dependencies
}

// NewTickTimers wires the use case.
func NewTickTimers(deps Dependencies) TickTimers {
	return TickTimers{deps: deps}
}

// teamTimer is a timer the members of a team declare with deliver: team.
type teamTimer struct {
	team      domain.TeamName
	name      string
	declarers []domain.Bot
	triggers  []domain.Trigger
}

// Execute runs one pass at the clock's now.
func (uc TickTimers) Execute(ctx context.Context) (TickTimersResult, error) {
	bots, err := uc.deps.Bots.List(ctx)
	if err != nil {
		return TickTimersResult{}, err
	}
	stored, err := uc.deps.Timers.List(ctx)
	if err != nil {
		return TickTimersResult{}, err
	}
	timers := make(map[string]domain.Timer, len(stored))
	for _, timer := range stored {
		timers[timer.Key()] = timer
	}
	live := map[string]bool{}
	teams := map[string]*teamTimer{}
	result := TickTimersResult{}
	for _, bot := range bots {
		for _, trigger := range bot.Triggers().Timers() {
			if trigger.Deliver().Team() {
				team := memoryTeamOf(bot)
				key := domain.TeamTimerKey(team, trigger.Name())
				if teams[key] == nil {
					teams[key] = &teamTimer{team: team, name: trigger.Name()}
				}
				teams[key].declarers = append(teams[key].declarers, bot)
				teams[key].triggers = append(teams[key].triggers, trigger)
				continue
			}
			key := domain.TimerKey(bot.ID(), trigger.Name())
			live[key] = true
			timer, found := timers[key]
			fire, err := uc.tick(ctx, bot, trigger, timer, found)
			if err != nil {
				result.Errors = append(result.Errors, TimerError{BotID: bot.ID(), Trigger: trigger.Name(), Err: err})
				continue
			}
			if fire != nil {
				result.Fired = append(result.Fired, *fire)
			}
		}
	}
	keys := make([]string, 0, len(teams))
	for key := range teams {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		live[key] = true
		t := teams[key]
		timer, found := timers[key]
		fire, err := uc.tickTeam(ctx, t, timer, found)
		if err != nil {
			result.Errors = append(result.Errors, TimerError{Team: t.team, Trigger: t.name, Err: err})
			continue
		}
		if fire != nil {
			result.Fired = append(result.Fired, *fire)
		}
	}
	for key := range timers {
		if !live[key] {
			if err := uc.deps.Timers.Delete(ctx, key); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}

// arm gives the timer its first slot when it has none (create makes it)
// and re-arms it when its schedule changed; dirty says it must be saved.
func arm(timer domain.Timer, found bool, schedule domain.Schedule, now time.Time, create func(first time.Time) (domain.Timer, error)) (domain.Timer, bool, error) {
	if !found {
		first := schedule.Next(now)
		if schedule.ArmsAtOnce() {
			first = now
		}
		created, err := create(first)
		return created, true, err
	}
	if timer.ScheduleKey() != schedule.Key() {
		if err := timer.Rearm(schedule.Key(), schedule.Next(now)); err != nil {
			return domain.Timer{}, false, err
		}
		return timer, true, nil
	}
	return timer, false, nil
}

// nextAfter is the schedule's slot after now.
func nextAfter(schedule domain.Schedule, now time.Time) (time.Time, error) {
	next := schedule.Next(now)
	if next.IsZero() {
		return time.Time{}, errors.New("the schedule " + schedule.Key() + " has no next fire")
	}
	return next, nil
}

// tick arms, re-arms, advances or fires one timer and saves it.
func (uc TickTimers) tick(ctx context.Context, bot domain.Bot, trigger domain.Trigger, timer domain.Timer, found bool) (*TimerFire, error) {
	now := uc.deps.Clock.Now()
	schedule := trigger.Schedule()
	if !found && !bot.AcceptsTasks() {
		return nil, nil
	}
	timer, dirty, err := arm(timer, found, schedule, now, func(first time.Time) (domain.Timer, error) {
		return domain.NewTimer(bot.ID(), trigger.Name(), schedule.Key(), first)
	})
	if err != nil {
		return nil, err
	}
	var fire *TimerFire
	if timer.Due(now) {
		next, err := nextAfter(schedule, now)
		if err != nil {
			return nil, err
		}
		if !bot.AcceptsTasks() {
			if err := timer.Rearm(schedule.Key(), next); err != nil {
				return nil, err
			}
		} else {
			fired, err := NewFireTrigger(uc.deps).Execute(ctx, FireTriggerCommand{BotID: bot.ID().String(), TriggerName: trigger.Name(), ScheduledAt: timer.NextFireAt()})
			if err != nil {
				return nil, err
			}
			fire = &TimerFire{BotID: bot.ID(), Trigger: trigger.Name(), ScheduledAt: timer.NextFireAt(), TaskID: fired.Task.ID(), Created: fired.Created}
			if err := timer.Fired(fired.Task.ID(), now, next); err != nil {
				return nil, err
			}
		}
		dirty = true
	}
	if dirty {
		if err := uc.deps.Timers.Save(ctx, timer); err != nil {
			return nil, err
		}
	}
	return fire, nil
}

// tickTeam arms, re-arms, advances or fires a team's timer and saves it:
// a due slot goes to the task it made already (a crash before the timer
// was saved), or to one member picked among the declarers that take
// tasks. Declarers whose schedules differ fire nothing.
func (uc TickTimers) tickTeam(ctx context.Context, t *teamTimer, timer domain.Timer, found bool) (*TimerFire, error) {
	now := uc.deps.Clock.Now()
	order := make([]int, len(t.declarers))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int {
		return strings.Compare(t.declarers[a].ID().String(), t.declarers[b].ID().String())
	})
	schedule := t.triggers[order[0]].Schedule()
	var takers []domain.Bot
	for _, i := range order {
		if key := t.triggers[i].Schedule().Key(); key != schedule.Key() {
			return nil, errors.New("the members of team " + t.team.String() + " give timer " + t.name + " different schedules: " + schedule.Key() + " and " + key)
		}
		if t.declarers[i].AcceptsTasks() {
			takers = append(takers, t.declarers[i])
		}
	}
	if !found && len(takers) == 0 {
		return nil, nil
	}
	timer, dirty, err := arm(timer, found, schedule, now, func(first time.Time) (domain.Timer, error) {
		return domain.NewTeamTimer(t.team, t.name, schedule.Key(), first)
	})
	if err != nil {
		return nil, err
	}
	var fire *TimerFire
	if timer.Due(now) {
		next, err := nextAfter(schedule, now)
		if err != nil {
			return nil, err
		}
		if len(takers) == 0 {
			if err := timer.Rearm(schedule.Key(), next); err != nil {
				return nil, err
			}
		} else {
			if fire, err = uc.fireTeam(ctx, t, takers, timer.NextFireAt()); err != nil {
				return nil, err
			}
			if err := timer.Fired(fire.TaskID, now, next); err != nil {
				return nil, err
			}
		}
		dirty = true
	}
	if dirty {
		if err := uc.deps.Timers.Save(ctx, timer); err != nil {
			return nil, err
		}
	}
	return fire, nil
}

// fireTeam gives the slot to the declarer that holds its task already,
// or fires the timer of the member picked.
func (uc TickTimers) fireTeam(ctx context.Context, t *teamTimer, takers []domain.Bot, slot time.Time) (*TimerFire, error) {
	key := domain.FireKeyTimer(t.name, slot)
	for _, bot := range t.declarers {
		task, found, err := uc.deps.Tasks.FindByIdempotencyKey(ctx, bot.ID(), key, time.Time{})
		if err != nil {
			return nil, err
		}
		if found {
			return &TimerFire{BotID: bot.ID(), Team: t.team, Trigger: t.name, ScheduledAt: slot, TaskID: task.ID()}, nil
		}
	}
	picked, err := pickMember(ctx, uc.deps, takers)
	if err != nil {
		return nil, err
	}
	fired, err := NewFireTrigger(uc.deps).Execute(ctx, FireTriggerCommand{BotID: picked.ID().String(), TriggerName: t.name, ScheduledAt: slot})
	if err != nil {
		return nil, err
	}
	return &TimerFire{BotID: picked.ID(), Team: t.team, Trigger: t.name, ScheduledAt: slot, TaskID: fired.Task.ID(), Created: fired.Created}, nil
}
