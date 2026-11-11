package domain

// EmitRuleSnapshot is one rule of what a bot may emit, as plain data: a
// name or pattern, and the other teams a domain event of that name is
// published to (docs/architecture/team-topics.md). MaxSeverity is read
// from older definitions and ignored: the name's kind fixes the
// severity.
type EmitRuleSnapshot struct {
	Name        string   `json:"name"`
	Teams       []string `json:"teams,omitempty"`
	MaxSeverity string   `json:"maxSeverity,omitempty"`
}
