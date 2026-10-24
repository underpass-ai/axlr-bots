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
}
