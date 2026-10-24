package domain

// NodeStatus is whether the control plane can count on a node.
type NodeStatus string

const (
	// NodeRegistered is a node that registered and has not been heard from since.
	NodeRegistered NodeStatus = "Registered"
	// NodeOnline is a node whose heartbeat is recent.
	NodeOnline NodeStatus = "Online"
	// NodeOffline is a node whose heartbeat stopped.
	NodeOffline NodeStatus = "Offline"
)

// ParseNodeStatus validates a status.
func ParseNodeStatus(raw string) (NodeStatus, error) {
	switch NodeStatus(raw) {
	case NodeRegistered, NodeOnline, NodeOffline:
		return NodeStatus(raw), nil
	}
	return "", invalid("node status", "is not a known node status")
}

func (s NodeStatus) String() string { return string(s) }
