package nodeproto

// EventsOfferMessage tells the node an execution's bot may emit events
// to its teams: the names or patterns it may use, the highest severity
// and the teammates an event may be addressed to. A node older than the
// field ignores it and offers no tool.
type EventsOfferMessage struct {
	Names       []string `json:"names"`
	MaxSeverity string   `json:"maxSeverity"`
	Teammates   []string `json:"teammates"`
}
