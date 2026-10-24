package application

import (
	"context"
	"errors"

	"github.com/underpass-ai/axlr-bots/domain"
)

// TickTimers is the scheduler's pass: for every timer trigger of every
// bot it arms a timer that has none (an interval fires at once, a cron
// at its next slot), re-arms one whose schedule changed, and fires the
// ones that are due, once each: the fire is for the persisted slot, so a
// daemon that was down fires the slot it missed once and arms the next
// one after now rather than replaying every slot in between. A bot that
// takes no tasks has its timers advanced, not fired. Timers of triggers
// that no longer exist are removed.
type TickTimers struct {
	deps Dependencies
}

// NewTickTimers wires the use case.
func NewTickTimers(deps Dependencies) TickTimers {
	return TickTimers{deps: deps}
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
	result := TickTimersResult{}
	for _, bot := range bots {
		for _, trigger := range bot.Triggers().Timers() {
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
	for key := range timers {
		if !live[key] {
			if err := uc.deps.Timers.Delete(ctx, key); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}

// tick arms, re-arms, advances or fires one timer and saves it.
func (uc TickTimers) tick(ctx context.Context, bot domain.Bot, trigger domain.Trigger, timer domain.Timer, found bool) (*TimerFire, error) {
	now := uc.deps.Clock.Now()
	schedule := trigger.Schedule()
	dirty := false
	if !found {
		first := schedule.Next(now)
		if schedule.ArmsAtOnce() {
			first = now
		}
		var err error
		if timer, err = domain.NewTimer(bot.ID(), trigger.Name(), schedule.Key(), first); err != nil {
			return nil, err
		}
		dirty = true
	} else if timer.ScheduleKey() != schedule.Key() {
		if err := timer.Rearm(schedule.Key(), schedule.Next(now)); err != nil {
			return nil, err
		}
		dirty = true
	}
	var fire *TimerFire
	if timer.Due(now) {
		next := schedule.Next(now)
		if next.IsZero() {
			return nil, errors.New("the schedule " + schedule.Key() + " has no next fire")
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
