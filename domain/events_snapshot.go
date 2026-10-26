package domain

// EventsSnapshot is what a bot may emit to its teams, as plain data: the
// rules (absent: any name up to warning) and the most events a minute
// (0: the default, 6).
type EventsSnapshot struct {
	Emit      []EmitRuleSnapshot `json:"emit,omitempty"`
	PerMinute int                `json:"perMinute,omitempty"`
}
