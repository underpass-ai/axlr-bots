package domain

// EventsSnapshot is what a bot may emit to its teams, as plain data: the
// rules (absent: any name but error.* and done.*), the most events a minute (0:
// the default, 6) and whether a teammate's warning may interrupt it
// (absent: it may).
type EventsSnapshot struct {
	Emit          []EmitRuleSnapshot `json:"emit,omitempty"`
	PerMinute     int                `json:"perMinute,omitempty"`
	Interruptible *bool              `json:"interruptible,omitempty"`
}
