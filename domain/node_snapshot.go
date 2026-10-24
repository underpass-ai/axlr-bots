package domain

import "time"

// NodeSnapshot is a node as plain data.
type NodeSnapshot struct {
	ID           string            `json:"id"`
	HostName     string            `json:"hostName"`
	Labels       map[string]string `json:"labels,omitempty"`
	Capabilities NodeCapabilities  `json:"capabilities"`
	Status       string            `json:"status"`
	RegisteredAt time.Time         `json:"registeredAt"`
	LastSeenAt   time.Time         `json:"lastSeenAt"`
	// CertificateSerial is the serial of the node's current client
	// certificate; empty for a node that talks over the local socket.
	CertificateSerial string `json:"certificateSerial,omitempty"`
	// CertificateNotAfter is when that certificate expires; absent until
	// the control plane has seen it.
	CertificateNotAfter *time.Time `json:"certificateNotAfter,omitempty"`
	RevokedAt           *time.Time `json:"revokedAt,omitempty"`
}
