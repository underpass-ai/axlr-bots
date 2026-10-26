package domain

// EmitRule lets a bot emit the events whose names fit a pattern, up to a
// severity.
type EmitRule struct {
	pattern     EventPattern
	maxSeverity Severity
}

// NewEmitRule validates a rule; the severity defaults to warning.
func NewEmitRule(s EmitRuleSnapshot) (EmitRule, error) {
	pattern, err := NewEventPattern(s.Name)
	if err != nil {
		return EmitRule{}, err
	}
	severity := SeverityWarning
	if s.MaxSeverity != "" {
		if severity, err = ParseSeverity(s.MaxSeverity); err != nil {
			return EmitRule{}, invalid("events emit "+s.Name, err.Error())
		}
	}
	return EmitRule{pattern: pattern, maxSeverity: severity}, nil
}

// Pattern is the names the rule lets the bot emit.
func (r EmitRule) Pattern() EventPattern { return r.pattern }

// MaxSeverity is the highest severity the rule allows.
func (r EmitRule) MaxSeverity() Severity { return r.maxSeverity }

// Snapshot returns the rule as plain data.
func (r EmitRule) Snapshot() EmitRuleSnapshot {
	return EmitRuleSnapshot{Name: r.pattern.String(), MaxSeverity: r.maxSeverity.String()}
}
