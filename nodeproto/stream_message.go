package nodeproto

import "time"

// StreamPingEvery is how often the control plane pings a node's stream
// when nothing else happens; a node that hears nothing for StreamSilence
// opens it again.
const (
	StreamPingEvery = 15 * time.Second
	StreamSilence   = 3 * StreamPingEvery
)

// Kinds of a line on a node's stream.
const (
	// StreamExecution: something happened to an execution the node runs
	// that it acts on at once (cancelled, preempted, expired, a message
	// reached it): the node renews its lease and relays its messages now.
	StreamExecution = "execution"
	// StreamResync: the node fell behind the hints the control plane
	// keeps: it renews every execution it runs now.
	StreamResync = "resync"
	// StreamPing keeps the stream alive and says the control plane is.
	StreamPing = "ping"
)

// StreamMessage is one line of a node's stream: a JSON object per line,
// pushed by the control plane as things happen.
type StreamMessage struct {
	Type        string `json:"type"`
	ExecutionID string `json:"executionId,omitempty"`
}
