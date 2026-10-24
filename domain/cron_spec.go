package domain

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// cronSpec is a five-field cron expression: minute, hour, day of month,
// month, day of week. Each field is `*`, a number, a range `a-b`, a
// step `*/n` or `a-b/n`, or a list of those; numbers only (Sunday is 0
// or 7). As in cron, when both the day of month and the day of week are
// restricted a day matching either fires.
type cronSpec struct {
	minute, hour, dom, month, dow uint64
	anyDom, anyDow                bool
	text                          string
}

// cronHorizon bounds the search for the next fire.
const cronHorizon = 5 * 366 * 24 * time.Hour

type cronField struct {
	name     string
	min, max int
}

var cronFields = [5]cronField{{"minute", 0, 59}, {"hour", 0, 23}, {"day of month", 1, 31}, {"month", 1, 12}, {"day of week", 0, 7}}

func parseCron(raw string) (*cronSpec, error) {
	parts := strings.Fields(raw)
	if len(parts) != 5 {
		return nil, errors.New("cron needs five fields: minute hour day-of-month month day-of-week")
	}
	spec := &cronSpec{text: strings.Join(parts, " ")}
	targets := [5]*uint64{&spec.minute, &spec.hour, &spec.dom, &spec.month, &spec.dow}
	for i, part := range parts {
		bits, any, err := parseCronField(cronFields[i], part)
		if err != nil {
			return nil, err
		}
		*targets[i] = bits
		switch i {
		case 2:
			spec.anyDom = any
		case 4:
			spec.anyDow = any
		}
	}
	// Sunday is 0 or 7: either sets both.
	if spec.dow&(1<<7) != 0 || spec.dow&1 != 0 {
		spec.dow |= 1 | 1<<7
	}
	return spec, nil
}

// parseCronField returns the set of values the field names and whether
// it was `*` (unrestricted).
func parseCronField(field cronField, raw string) (uint64, bool, error) {
	var bits uint64
	unrestricted := false
	for _, item := range strings.Split(raw, ",") {
		step := 1
		if base, stepText, found := strings.Cut(item, "/"); found {
			n, err := strconv.Atoi(stepText)
			if err != nil || n < 1 {
				return 0, false, errors.New("cron " + field.name + ": the step after / must be a positive number")
			}
			step, item = n, base
		}
		lo, hi := field.min, field.max
		switch {
		case item == "*":
			if step == 1 {
				unrestricted = true
			}
		case strings.Contains(item, "-"):
			a, b, _ := strings.Cut(item, "-")
			var err error
			if lo, err = cronNumber(field, a); err != nil {
				return 0, false, err
			}
			if hi, err = cronNumber(field, b); err != nil {
				return 0, false, err
			}
			if lo > hi {
				return 0, false, errors.New("cron " + field.name + ": the range " + item + " is backwards")
			}
		default:
			n, err := cronNumber(field, item)
			if err != nil {
				return 0, false, err
			}
			lo, hi = n, n
			if step != 1 {
				hi = field.max
			}
		}
		for v := lo; v <= hi; v += step {
			bits |= 1 << uint(v)
		}
	}
	return bits, unrestricted, nil
}

func cronNumber(field cronField, raw string) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil || n < field.min || n > field.max {
		return 0, errors.New("cron " + field.name + ": " + raw + " is not a number from " + strconv.Itoa(field.min) + " to " + strconv.Itoa(field.max))
	}
	return n, nil
}

func (c *cronSpec) has(bits uint64, v int) bool { return bits&(1<<uint(v)) != 0 }

// dayMatches applies cron's rule for the two day fields.
func (c *cronSpec) dayMatches(t time.Time) bool {
	dom := c.has(c.dom, t.Day())
	dow := c.has(c.dow, int(t.Weekday()))
	switch {
	case c.anyDom && c.anyDow:
		return true
	case c.anyDom:
		return dow
	case c.anyDow:
		return dom
	}
	return dom || dow
}

// next is the first minute strictly after the instant that matches, in
// the instant's location; zero when none within the horizon.
func (c *cronSpec) next(after time.Time) time.Time {
	t := after.Truncate(time.Minute).Add(time.Minute)
	limit := after.Add(cronHorizon)
	for t.Before(limit) {
		switch {
		case !c.has(c.month, int(t.Month())):
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
		case !c.dayMatches(t):
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
		case !c.has(c.hour, t.Hour()):
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
		case !c.has(c.minute, t.Minute()):
			t = t.Add(time.Minute)
		default:
			return t
		}
	}
	return time.Time{}
}
