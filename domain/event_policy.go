package domain

// Limits of what a bot may emit.
const (
	// DefaultEventsPerMinute is how many events a bot may emit in a
	// minute when its definition names no rate.
	DefaultEventsPerMinute = 6
	// MaxEventsPerMinute is the highest rate a definition may name.
	MaxEventsPerMinute = 60
	maxEmitRules       = 16
)

// EventPolicy is what a bot may emit to its teams. The zero value is the
// team's default: any name but the control plane's kinds, 6 a minute.
type EventPolicy struct {
	rules     []EmitRule
	perMinute int
	steady    bool
}

// NewEventPolicy validates a bot's events: up to 16 rules, a rate from 1
// to 60 (0: the default).
func NewEventPolicy(s EventsSnapshot) (EventPolicy, error) {
	if len(s.Emit) > maxEmitRules {
		return EventPolicy{}, invalid("events emit", "names at most 16 rules")
	}
	if s.PerMinute < 0 || s.PerMinute > MaxEventsPerMinute {
		return EventPolicy{}, invalid("events perMinute", "must be from 1 to 60")
	}
	p := EventPolicy{perMinute: s.PerMinute, steady: s.Interruptible != nil && !*s.Interruptible}
	for _, raw := range s.Emit {
		rule, err := NewEmitRule(raw)
		if err != nil {
			return EventPolicy{}, err
		}
		p.rules = append(p.rules, rule)
	}
	return p, nil
}

// Rules are what the bot may emit: its definition's, or any name.
func (p EventPolicy) Rules() []EmitRule {
	if len(p.rules) == 0 {
		return []EmitRule{{pattern: AnyEvent}}
	}
	return append([]EmitRule(nil), p.rules...)
}

// PerMinute is how many events the bot may emit in a minute.
func (p EventPolicy) PerMinute() int {
	if p.perMinute == 0 {
		return DefaultEventsPerMinute
	}
	return p.perMinute
}

// MayEmit reports whether the bot may emit the name: a bot never
// publishes the control plane's kinds (error, done), and otherwise a
// rule must fit the name.
func (p EventPolicy) MayEmit(name EventName) bool {
	if !KindOf(name).PublishedBy(PublisherBot) {
		return false
	}
	for _, rule := range p.Rules() {
		if rule.Pattern().Matches(name) {
			return true
		}
	}
	return false
}

// Interruptible reports whether a teammate's warning may interrupt the
// bot; true unless its definition says interruptible false.
func (p EventPolicy) Interruptible() bool { return !p.steady }

// Snapshot returns the policy as plain data, nil when it is the default.
func (p EventPolicy) Snapshot() *EventsSnapshot {
	if len(p.rules) == 0 && p.perMinute == 0 && !p.steady {
		return nil
	}
	s := &EventsSnapshot{PerMinute: p.perMinute}
	if p.steady {
		steady := false
		s.Interruptible = &steady
	}
	for _, rule := range p.rules {
		s.Emit = append(s.Emit, rule.Snapshot())
	}
	return s
}
