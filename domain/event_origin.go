package domain

// MaxTeamCrossings is how many team boundaries one trace may cross; the
// next crossing is refused (RefusedCrossingLimit).
const MaxTeamCrossings = 4

// EventOrigin is where a task made by another team's domain event came
// from: the event, the bot that published it, its team, the trace and
// how many team boundaries the trace has crossed with this one, and the
// KMP key of its context when the event named one. Such a
// task starts a new chain in its own team (depth 0); the origin keeps the
// way back for the audit (docs/architecture/team-topics.md).
type EventOrigin struct {
	EventID   string `json:"eventId"`
	From      string `json:"from"`
	Team      string `json:"team"`
	TraceID   string `json:"traceId,omitempty"`
	Crossings int    `json:"crossings"`
	Context   string `json:"context,omitempty"`
}

// Validate checks the origin's invariants.
func (o EventOrigin) Validate() error {
	if _, err := NewEventID(o.EventID); err != nil {
		return invalid("origin event", err.Error())
	}
	if _, err := NewBotID(o.From); err != nil {
		return invalid("origin from", err.Error())
	}
	if _, err := NewTeamName(o.Team); err != nil {
		return invalid("origin team", err.Error())
	}
	if o.Crossings < 1 || o.Crossings > MaxTeamCrossings {
		return invalid("origin crossings", "must be from 1 to 4")
	}
	if o.Context != "" {
		if _, err := NewContextKey(o.Context); err != nil {
			return invalid("origin context", err.Error())
		}
	}
	return nil
}

// NextCrossings is how many boundaries an event crosses when a task with
// the origin (nil: none) publishes it to another team; false when that
// would pass MaxTeamCrossings.
func NextCrossings(origin *EventOrigin) (int, bool) {
	next := 1
	if origin != nil {
		next = origin.Crossings + 1
	}
	return next, next <= MaxTeamCrossings
}
