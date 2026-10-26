package domain

// EmitRuleSnapshot is one rule of what a bot may emit, as plain data: a
// name or pattern and the highest severity (warning when not given).
type EmitRuleSnapshot struct {
	Name        string `json:"name"`
	MaxSeverity string `json:"maxSeverity,omitempty"`
}
