package domain

import "time"

// NodeSnapshot is a node as plain data.
type NodeSnapshot struct {
	ID       string `json:"id"`
	HostName string `json:"hostName"`
	// Labels are the effective labels placement matches: the reported
	// ones with the operator's on top.
	Labels map[string]string `json:"labels,omitempty"`
	// OperatorLabels are the enrollment token's labels; they win.
	OperatorLabels map[string]string `json:"operatorLabels,omitempty"`
	// ReportedLabels are the labels the agent last registered with.
	ReportedLabels map[string]string `json:"reportedLabels,omitempty"`
	Capabilities   NodeCapabilities  `json:"capabilities"`
	Status         string            `json:"status"`
	RegisteredAt   time.Time         `json:"registeredAt"`
	LastSeenAt     time.Time         `json:"lastSeenAt"`
	// CertificateSerial is the serial of the node's current client
	// certificate; empty for a node that talks over the local socket.
	CertificateSerial string `json:"certificateSerial,omitempty"`
	// CertificateNotAfter is when that certificate expires; absent until
	// the control plane has seen it.
	CertificateNotAfter *time.Time `json:"certificateNotAfter,omitempty"`
	RevokedAt           *time.Time `json:"revokedAt,omitempty"`
}
