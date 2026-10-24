package domain

import (
	"errors"
	"time"
)

// MinInterval is the shortest interval a timer may run on.
const MinInterval = time.Second

// Schedule is when a timer fires: every so often, or on a cron
// expression evaluated in the host's time zone.
type Schedule struct {
	every time.Duration
	cron  *cronSpec
	text  string
}

// NewSchedule validates a schedule: exactly one of an interval ("10m",
// at least a second) or a five-field cron expression.
func NewSchedule(every, cron string) (Schedule, error) {
	switch {
	case every == "" && cron == "":
		return Schedule{}, errors.New("a timer needs every (a duration) or cron (five fields)")
	case every != "" && cron != "":
		return Schedule{}, errors.New("a timer takes every or cron, not both")
	case every != "":
		d, err := time.ParseDuration(every)
		if err != nil {
			return Schedule{}, errors.New("every must be a duration such as 10m")
		}
		if d < MinInterval {
			return Schedule{}, errors.New("every must be at least " + MinInterval.String())
		}
		return Schedule{every: d, text: d.String()}, nil
	}
	spec, err := parseCron(cron)
	if err != nil {
		return Schedule{}, err
	}
	return Schedule{cron: spec, text: spec.text}, nil
}

// IsZero reports a schedule that was never set.
func (s Schedule) IsZero() bool { return s.every == 0 && s.cron == nil }

// Key names the schedule, so a changed definition is told from the one
// a timer was armed with.
func (s Schedule) Key() string {
	if s.cron != nil {
		return "cron " + s.text
	}
	return "every " + s.text
}

// Next is the first fire strictly after the instant.
func (s Schedule) Next(after time.Time) time.Time {
	if s.cron != nil {
		return s.cron.next(after)
	}
	return after.Add(s.every)
}

// ArmsAtOnce reports a schedule whose first fire is when it is armed:
// an interval runs now and then every so often; a cron waits for its
// next slot.
func (s Schedule) ArmsAtOnce() bool { return s.cron == nil }

// Snapshot returns the interval and the cron expression, one of them
// empty.
func (s Schedule) Snapshot() (every, cron string) {
	if s.cron != nil {
		return "", s.text
	}
	if s.every == 0 {
		return "", ""
	}
	return s.text, ""
}
