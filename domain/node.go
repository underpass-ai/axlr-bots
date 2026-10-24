package domain

import "time"

// Node is a host the node agent runs on: it registers with the control
// plane, heartbeats, and asks for the executions of the bots placed on
// it. A node is a resource of the control plane, as a bot is; the control
// plane never reaches into the host.
type Node struct {
	id           NodeID
	hostName     string
	labels       NodeLabels
	capabilities NodeCapabilities
	status       NodeStatus
	registeredAt time.Time
	lastSeenAt   time.Time
}

// NewNode registers a node.
func NewNode(id NodeID, hostName string, labels NodeLabels, capabilities NodeCapabilities, now time.Time) (Node, error) {
	host, err := validateText("host name", hostName, 253)
	if err != nil {
		return Node{}, err
	}
	if now.IsZero() {
		return Node{}, invalid("registered at", "must not be zero")
	}
	return Node{id: id, hostName: host, labels: labels, capabilities: capabilities, status: NodeRegistered, registeredAt: now, lastSeenAt: now}, nil
}

// ID is the node's identifier.
func (n Node) ID() NodeID { return n.id }

// HostName is the host the node reported.
func (n Node) HostName() string { return n.hostName }

// Labels are the node's labels.
func (n Node) Labels() NodeLabels { return n.labels }

// Capabilities is what the node can run.
func (n Node) Capabilities() NodeCapabilities { return n.capabilities }

// Status is the node's liveness.
func (n Node) Status() NodeStatus { return n.status }

// RegisteredAt is when the node first registered.
func (n Node) RegisteredAt() time.Time { return n.registeredAt }

// LastSeenAt is the node's last registration or heartbeat.
func (n Node) LastSeenAt() time.Time { return n.lastSeenAt }

// IsOnline reports a node the control plane may assign work to.
func (n Node) IsOnline() bool { return n.status == NodeOnline }

// Describe takes a node's registration again: a restarted agent reports
// its host, labels and capabilities anew and comes Online.
func (n *Node) Describe(hostName string, labels NodeLabels, capabilities NodeCapabilities, now time.Time) error {
	host, err := validateText("host name", hostName, 253)
	if err != nil {
		return err
	}
	n.hostName = host
	n.labels = labels
	n.capabilities = capabilities
	n.status = NodeOnline
	n.lastSeenAt = now
	return nil
}

// Heartbeat marks the node Online as of now.
func (n *Node) Heartbeat(now time.Time) {
	n.status = NodeOnline
	n.lastSeenAt = now
}

// MarkOffline records that the node's heartbeat stopped; lastSeenAt
// stays the last heartbeat.
func (n *Node) MarkOffline() { n.status = NodeOffline }

// SeenWithin reports a heartbeat within the duration of now.
func (n Node) SeenWithin(now time.Time, within time.Duration) bool {
	return !now.After(n.lastSeenAt.Add(within))
}

// Snapshot returns the node as plain data.
func (n Node) Snapshot() NodeSnapshot {
	return NodeSnapshot{
		ID: n.id.String(), HostName: n.hostName, Labels: n.labels.Copy(), Capabilities: n.capabilities,
		Status: n.status.String(), RegisteredAt: n.registeredAt, LastSeenAt: n.lastSeenAt,
	}
}

// RestoreNode rebuilds a node from a snapshot.
func RestoreNode(s NodeSnapshot) (Node, error) {
	id, err := NewNodeID(s.ID)
	if err != nil {
		return Node{}, err
	}
	labels, err := NewNodeLabels(s.Labels)
	if err != nil {
		return Node{}, err
	}
	status, err := ParseNodeStatus(s.Status)
	if err != nil {
		return Node{}, err
	}
	node, err := NewNode(id, s.HostName, labels, s.Capabilities, s.RegisteredAt)
	if err != nil {
		return Node{}, err
	}
	if s.LastSeenAt.IsZero() {
		return Node{}, invalid("last seen at", "must not be zero")
	}
	node.status = status
	node.lastSeenAt = s.LastSeenAt
	return node, nil
}
