package domain

// EventsSnapshot is what a bot may emit to its teams and how it hears
// them, as plain data: the rules (absent: any name but error.* and
// done.*), the most events a minute (0: the default, 6), the most events
// a minute it takes as tasks or messages (0: the default, 10), how deep
// a chain an event of it starts may go (0: the default, 2) and whether a
// teammate's critical may interrupt it (absent: it may).
type EventsSnapshot struct {
	Emit            []EmitRuleSnapshot `json:"emit,omitempty"`
	PerMinute       int                `json:"perMinute,omitempty"`
	IntakePerMinute int                `json:"intakePerMinute,omitempty"`
	MaxDepth        int                `json:"maxDepth,omitempty"`
	Interruptible   *bool              `json:"interruptible,omitempty"`
}
