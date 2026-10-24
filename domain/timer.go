package domain

import "time"

// Timer is the durable state of one timer trigger: when it fires next.
// It is stored apart from the bot so a fire and its persisted successor
// survive a restart: a daemon that comes back finds NextFireAt in the
// past and fires once for it, with the idempotency key that instant
// gives, then arms the next fire after now. Missed slots in between are
// not replayed.
type Timer struct {
	botID        BotID
	trigger      string
	scheduleKey  string
	nextFireAt   time.Time
	lastFiredAt  time.Time
	lastSchedule time.Time
	lastTaskID   TaskID
}

// NewTimer arms a timer for the first time.
func NewTimer(bot BotID, trigger, scheduleKey string, next time.Time) (Timer, error) {
	if bot == "" || trigger == "" || scheduleKey == "" {
		return Timer{}, invalid("timer", "needs a bot, a trigger and a schedule")
	}
	if next.IsZero() {
		return Timer{}, invalid("next fire at", "must not be zero")
	}
	return Timer{botID: bot, trigger: trigger, scheduleKey: scheduleKey, nextFireAt: next}, nil
}

// TimerKey names a timer in a repository: the bot and the trigger.
func TimerKey(bot BotID, trigger string) string { return bot.String() + "~" + trigger }

// Key is the timer's repository key.
func (t Timer) Key() string { return TimerKey(t.botID, t.trigger) }

// BotID is the bot the timer belongs to.
func (t Timer) BotID() BotID { return t.botID }

// Trigger is the timer trigger's name.
func (t Timer) Trigger() string { return t.trigger }

// ScheduleKey is the schedule the timer was armed with.
func (t Timer) ScheduleKey() string { return t.scheduleKey }

// NextFireAt is the instant of the next fire.
func (t Timer) NextFireAt() time.Time { return t.nextFireAt }

// LastFiredAt is when the timer last fired; zero before the first fire.
func (t Timer) LastFiredAt() time.Time { return t.lastFiredAt }

// LastScheduledAt is the slot the last fire was for.
func (t Timer) LastScheduledAt() time.Time { return t.lastSchedule }

// LastTaskID is the task the last fire named.
func (t Timer) LastTaskID() TaskID { return t.lastTaskID }

// Due reports whether the timer should fire now.
func (t Timer) Due(now time.Time) bool { return !now.Before(t.nextFireAt) }

// Fired records a fire for the slot NextFireAt and arms the next one.
func (t *Timer) Fired(task TaskID, now, next time.Time) error {
	if next.IsZero() || !next.After(t.nextFireAt) {
		return invalid("next fire at", "must come after the fire")
	}
	t.lastFiredAt, t.lastSchedule, t.lastTaskID = now, t.nextFireAt, task
	t.nextFireAt = next
	return nil
}

// Rearm resets the timer for a changed schedule, or skips ahead without
// firing (a bot that takes no tasks).
func (t *Timer) Rearm(scheduleKey string, next time.Time) error {
	if scheduleKey == "" || next.IsZero() {
		return invalid("timer", "needs a schedule and a next fire")
	}
	t.scheduleKey, t.nextFireAt = scheduleKey, next
	return nil
}

// Snapshot returns the timer as plain data.
func (t Timer) Snapshot() TimerSnapshot {
	s := TimerSnapshot{BotID: t.botID.String(), Trigger: t.trigger, Schedule: t.scheduleKey, NextFireAt: t.nextFireAt, LastTaskID: t.lastTaskID.String()}
	if !t.lastFiredAt.IsZero() {
		fired, scheduled := t.lastFiredAt, t.lastSchedule
		s.LastFiredAt, s.LastScheduledAt = &fired, &scheduled
	}
	return s
}

// RestoreTimer rebuilds a timer from a snapshot.
func RestoreTimer(s TimerSnapshot) (Timer, error) {
	bot, err := NewBotID(s.BotID)
	if err != nil {
		return Timer{}, err
	}
	trigger, err := validateIdentifier("trigger name", s.Trigger)
	if err != nil {
		return Timer{}, err
	}
	timer, err := NewTimer(bot, trigger, s.Schedule, s.NextFireAt)
	if err != nil {
		return Timer{}, err
	}
	if s.LastFiredAt != nil {
		timer.lastFiredAt = *s.LastFiredAt
	}
	if s.LastScheduledAt != nil {
		timer.lastSchedule = *s.LastScheduledAt
	}
	if s.LastTaskID != "" {
		if timer.lastTaskID, err = NewTaskID(s.LastTaskID); err != nil {
			return Timer{}, err
		}
	}
	return timer, nil
}
