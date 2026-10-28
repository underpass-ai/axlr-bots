package nodeproto

// EventsOfferMessage tells the node an execution's bot may emit events
// to its teams: the names or patterns it may use and the teammates an
// event may be addressed to. A node older than the field ignores it and
// offers no tool. MaxSeverity is only for a node older than topic kinds,
// which still offers a severity: it is always critical, so such a node
// does not narrow what the model may send, and the control plane
// ignores what it sends.
type EventsOfferMessage struct {
	Names       []string `json:"names"`
	MaxSeverity string   `json:"maxSeverity,omitempty"`
	Teammates   []string `json:"teammates"`
}

// legacyMaxSeverity is what a node older than topic kinds is offered.
const legacyMaxSeverity = "critical"
