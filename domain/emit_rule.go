package domain

import "strings"

// EmitRule lets a bot emit the events whose names fit a pattern; the
// name's kind fixes the severity. A rule may also name other teams: an
// event of that name is a domain event that crosses to them
// (docs/architecture/team-topics.md).
type EmitRule struct {
	pattern EventPattern
	teams   []TeamName
}

// NewEmitRule validates a rule; a maxSeverity in it is accepted and
// ignored, since the kind decides. A rule with teams names one plain
// name of the info kind: only a domain event crosses, never a control
// signal (stop, danger, warn, error, done).
func NewEmitRule(s EmitRuleSnapshot) (EmitRule, error) {
	pattern, err := NewEventPattern(s.Name)
	if err != nil {
		return EmitRule{}, err
	}
	rule := EmitRule{pattern: pattern}
	if len(s.Teams) == 0 {
		return rule, nil
	}
	field := "events emit " + pattern.String() + " teams"
	if strings.Contains(pattern.String(), "*") {
		return EmitRule{}, invalid(field, "a domain event that crosses is one plain name, not a pattern")
	}
	name, err := NewEventName(pattern.String())
	if err != nil {
		return EmitRule{}, invalid(field, err.Error())
	}
	if KindOf(name) != KindInfo {
		return EmitRule{}, invalid(field, "only a domain event crosses: an info name, never stop, danger, warn, error or done")
	}
	if rule.teams, err = parseTeamList(field, s.Teams); err != nil {
		return EmitRule{}, err
	}
	return rule, nil
}

// Pattern is the names the rule lets the bot emit.
func (r EmitRule) Pattern() EventPattern { return r.pattern }

// Teams are the other teams an event of the rule's name crosses to;
// empty: the bot's own teams only.
func (r EmitRule) Teams() []TeamName { return append([]TeamName(nil), r.teams...) }

// Snapshot returns the rule as plain data.
func (r EmitRule) Snapshot() EmitRuleSnapshot {
	s := EmitRuleSnapshot{Name: r.pattern.String()}
	for _, team := range r.teams {
		s.Teams = append(s.Teams, team.String())
	}
	return s
}
