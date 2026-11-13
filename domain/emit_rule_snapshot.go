package domain

// EmitRuleSnapshot is one rule of what a bot may emit, as plain data: a
// name or pattern. MaxSeverity is read from older definitions and
// ignored: the name's kind fixes the severity.
type EmitRuleSnapshot struct {
	Name        string `json:"name"`
	MaxSeverity string `json:"maxSeverity,omitempty"`
}
