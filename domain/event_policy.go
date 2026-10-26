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
// team's default: any name, severity up to warning, 6 a minute.
type EventPolicy struct {
	rules     []EmitRule
	perMinute int
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
	p := EventPolicy{perMinute: s.PerMinute}
	for _, raw := range s.Emit {
		rule, err := NewEmitRule(raw)
		if err != nil {
			return EventPolicy{}, err
		}
		p.rules = append(p.rules, rule)
	}
	return p, nil
}

// Rules are what the bot may emit: its definition's, or any name up to
// warning.
func (p EventPolicy) Rules() []EmitRule {
	if len(p.rules) == 0 {
		return []EmitRule{{pattern: AnyEvent, maxSeverity: SeverityWarning}}
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

// MaxSeverity is the highest severity the bot may give the event, and
// false when no rule lets it emit the name at all.
func (p EventPolicy) MaxSeverity(name EventName) (Severity, bool) {
	best, found := SeverityInfo, false
	for _, rule := range p.Rules() {
		if rule.Pattern().Matches(name) {
			if !found || rule.MaxSeverity().AtLeast(best) {
				best = rule.MaxSeverity()
			}
			found = true
		}
	}
	return best, found
}

// Highest is the highest severity any rule allows: what the emit tool
// offers the model.
func (p EventPolicy) Highest() Severity {
	best := SeverityInfo
	for _, rule := range p.Rules() {
		if rule.MaxSeverity().AtLeast(best) {
			best = rule.MaxSeverity()
		}
	}
	return best
}

// Snapshot returns the policy as plain data, nil when it is the default.
func (p EventPolicy) Snapshot() *EventsSnapshot {
	if len(p.rules) == 0 && p.perMinute == 0 {
		return nil
	}
	s := &EventsSnapshot{PerMinute: p.perMinute}
	for _, rule := range p.rules {
		s.Emit = append(s.Emit, rule.Snapshot())
	}
	return s
}
