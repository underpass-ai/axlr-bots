package application

// EmitEventToolName is the tool the node serves to an execution whose
// bot is in a team; the control plane adds it to the execution's allowed
// tools, so the policy names it like any other.
const EmitEventToolName = "axlr_bots_emit_event"

// MaxEventData bounds the facts an event carries, as JSON.
const MaxEventData = 8 << 10

// EventsOffer tells the node that the execution's bot may emit events to
// its teams: the names or patterns it may use, the highest severity, and
// the teammates an event may be addressed to. The control plane checks
// every call again; the offer only describes the tool.
type EventsOffer struct {
	Names       []string
	MaxSeverity string
	Teammates   []string
}
