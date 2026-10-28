package domain

// EmitRule lets a bot emit the events whose names fit a pattern; the
// name's kind fixes the severity.
type EmitRule struct {
	pattern EventPattern
}

// NewEmitRule validates a rule; a maxSeverity in it is accepted and
// ignored, since the kind decides.
func NewEmitRule(s EmitRuleSnapshot) (EmitRule, error) {
	pattern, err := NewEventPattern(s.Name)
	if err != nil {
		return EmitRule{}, err
	}
	return EmitRule{pattern: pattern}, nil
}

// Pattern is the names the rule lets the bot emit.
func (r EmitRule) Pattern() EventPattern { return r.pattern }

// Snapshot returns the rule as plain data.
func (r EmitRule) Snapshot() EmitRuleSnapshot {
	return EmitRuleSnapshot{Name: r.pattern.String()}
}
