package domain

import "time"

// Node is a host the node agent runs on: it registers with the control
// plane, heartbeats, and asks for the executions of the bots placed on
// it. A node is a resource of the control plane, as a bot is; the control
// plane never reaches into the host. A node on another host holds a
// client certificate whose common name is its id; the node keeps the
// serial of the current one, and a revoked node is refused whatever it
// presents until it is enrolled again. Its labels come from two places:
// the operator's, on the enrollment token, and the ones the agent
// reports when it registers; the operator's win, so a node cannot move
// itself out of the zone it was enrolled into.
type Node struct {
	id                NodeID
	hostName          string
	operatorLabels    NodeLabels
	reportedLabels    NodeLabels
	capabilities      NodeCapabilities
	status            NodeStatus
	registeredAt      time.Time
	lastSeenAt        time.Time
	certificateSerial string
	certificateExpiry time.Time
	revokedAt         time.Time
}

// NewNode registers a node with the labels its agent reported.
func NewNode(id NodeID, hostName string, labels NodeLabels, capabilities NodeCapabilities, now time.Time) (Node, error) {
	host, err := validateText("host name", hostName, 253)
	if err != nil {
		return Node{}, err
	}
	if now.IsZero() {
		return Node{}, invalid("registered at", "must not be zero")
	}
	return Node{id: id, hostName: host, reportedLabels: labels, capabilities: capabilities, status: NodeRegistered, registeredAt: now, lastSeenAt: now}, nil
}

// ID is the node's identifier.
func (n Node) ID() NodeID { return n.id }

// HostName is the host the node reported.
func (n Node) HostName() string { return n.hostName }

// Labels are the node's effective labels: the ones its agent reported,
// with the operator's on top wherever both name a key.
func (n Node) Labels() NodeLabels {
	if len(n.operatorLabels) == 0 && len(n.reportedLabels) == 0 {
		return nil
	}
	labels := make(NodeLabels, len(n.operatorLabels)+len(n.reportedLabels))
	for k, v := range n.reportedLabels {
		labels[k] = v
	}
	for k, v := range n.operatorLabels {
		labels[k] = v
	}
	return labels
}

// OperatorLabels are the labels of the node's enrollment token.
func (n Node) OperatorLabels() NodeLabels { return n.operatorLabels }

// ReportedLabels are the labels the node's agent last reported.
func (n Node) ReportedLabels() NodeLabels { return n.reportedLabels }

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

// IsRevoked reports a node the operator cut off.
func (n Node) IsRevoked() bool { return n.status == NodeRevoked }

// CertificateSerial is the serial of the node's current certificate;
// empty for a node that was never enrolled.
func (n Node) CertificateSerial() string { return n.certificateSerial }

// RevokedAt is when the node was revoked; zero otherwise.
func (n Node) RevokedAt() time.Time { return n.revokedAt }

// Describe takes a node's registration again: a restarted agent reports
// its host, labels and capabilities anew and comes Online. A
// registration without labels keeps the ones the node reported before;
// the operator's labels stay whatever the node reports. A revoked node
// is refused.
func (n *Node) Describe(hostName string, labels NodeLabels, capabilities NodeCapabilities, now time.Time) error {
	if n.IsRevoked() {
		return n.refuse("register")
	}
	host, err := validateText("host name", hostName, 253)
	if err != nil {
		return err
	}
	n.hostName = host
	if len(labels) > 0 {
		n.reportedLabels = labels
	}
	n.capabilities = capabilities
	n.status = NodeOnline
	n.lastSeenAt = now
	return nil
}

// Heartbeat marks the node Online as of now; a revoked node is refused.
func (n *Node) Heartbeat(now time.Time) error {
	if n.IsRevoked() {
		return n.refuse("heartbeat")
	}
	n.status = NodeOnline
	n.lastSeenAt = now
	return nil
}

// Certify records the certificate the node was issued: the serial of the
// one the control plane now expects. A revoked node that is enrolled
// again is readmitted as Registered; an enrolled node that enrols again
// rotates its certificate and the previous one stops being accepted.
// Labels, when given, become the operator's; none keeps the ones the
// node has.
func (n *Node) Certify(serial string, labels NodeLabels, now time.Time) error {
	if serial == "" {
		return invalid("certificate serial", "must not be empty")
	}
	if serial != n.certificateSerial {
		n.certificateExpiry = time.Time{}
	}
	n.certificateSerial = serial
	if len(labels) > 0 {
		n.operatorLabels = labels
	}
	if n.IsRevoked() {
		n.status = NodeRegistered
		n.revokedAt = time.Time{}
	}
	n.lastSeenAt = now
	return nil
}

// CertificateExpiry is when the node's current certificate stops being
// valid; zero until the control plane has seen it (at enrollment, or on
// the node's next call over mutual TLS).
func (n Node) CertificateExpiry() time.Time { return n.certificateExpiry }

// RecordCertificateExpiry keeps the expiry of the certificate with the
// serial when it is the node's current one, and reports whether the
// node changed. A certificate the node no longer holds is ignored.
func (n *Node) RecordCertificateExpiry(serial string, notAfter time.Time) bool {
	if serial == "" || serial != n.certificateSerial || notAfter.IsZero() || notAfter.Equal(n.certificateExpiry) {
		return false
	}
	n.certificateExpiry = notAfter
	return true
}

// Revoke cuts the node off as of now.
func (n *Node) Revoke(now time.Time) error {
	if n.IsRevoked() {
		return n.refuse("revoke")
	}
	if now.IsZero() {
		return invalid("revoked at", "must not be zero")
	}
	n.status = NodeRevoked
	n.revokedAt = now
	return nil
}

// AcceptsCertificate reports whether a certificate with the serial is
// the node's current one: the node is not revoked and the serial is the
// one recorded at its last enrollment.
func (n Node) AcceptsCertificate(serial string) bool {
	return !n.IsRevoked() && n.certificateSerial != "" && n.certificateSerial == serial
}

func (n Node) refuse(attempted string) error {
	return StateError{Subject: "node " + n.id.String(), Current: n.status.String(), Attempted: attempted}
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
	s := NodeSnapshot{
		ID: n.id.String(), HostName: n.hostName, Labels: n.Labels().Copy(), Capabilities: n.capabilities,
		OperatorLabels: n.operatorLabels.Copy(), ReportedLabels: n.reportedLabels.Copy(),
		Status: n.status.String(), RegisteredAt: n.registeredAt, LastSeenAt: n.lastSeenAt, CertificateSerial: n.certificateSerial,
	}
	if !n.certificateExpiry.IsZero() {
		expiry := n.certificateExpiry
		s.CertificateNotAfter = &expiry
	}
	if !n.revokedAt.IsZero() {
		revoked := n.revokedAt
		s.RevokedAt = &revoked
	}
	return s
}

// RestoreNode rebuilds a node from a snapshot.
func RestoreNode(s NodeSnapshot) (Node, error) {
	id, err := NewNodeID(s.ID)
	if err != nil {
		return Node{}, err
	}
	operator, reported, err := restoreNodeLabels(s)
	if err != nil {
		return Node{}, err
	}
	status, err := ParseNodeStatus(s.Status)
	if err != nil {
		return Node{}, err
	}
	node, err := NewNode(id, s.HostName, reported, s.Capabilities, s.RegisteredAt)
	if err != nil {
		return Node{}, err
	}
	node.operatorLabels = operator
	if s.LastSeenAt.IsZero() {
		return Node{}, invalid("last seen at", "must not be zero")
	}
	node.status = status
	node.lastSeenAt = s.LastSeenAt
	node.certificateSerial = s.CertificateSerial
	if s.CertificateNotAfter != nil {
		node.certificateExpiry = *s.CertificateNotAfter
	}
	if s.RevokedAt != nil {
		node.revokedAt = *s.RevokedAt
	}
	if status == NodeRevoked && node.revokedAt.IsZero() {
		return Node{}, invalid("revoked at", "must be set on a revoked node")
	}
	return node, nil
}

// restoreNodeLabels splits a snapshot's labels into the operator's and
// the reported ones. A record written before the two were kept apart
// has only the effective labels: an enrolled node's were set by its
// enrollment and are the operator's, any other node's were reported.
func restoreNodeLabels(s NodeSnapshot) (operator, reported NodeLabels, err error) {
	if s.OperatorLabels == nil && s.ReportedLabels == nil {
		labels, err := NewNodeLabels(s.Labels)
		if err != nil {
			return nil, nil, err
		}
		if s.CertificateSerial != "" {
			return labels, nil, nil
		}
		return nil, labels, nil
	}
	if operator, err = NewNodeLabels(s.OperatorLabels); err != nil {
		return nil, nil, err
	}
	if reported, err = NewNodeLabels(s.ReportedLabels); err != nil {
		return nil, nil, err
	}
	return operator, reported, nil
}
