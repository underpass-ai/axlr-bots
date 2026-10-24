package nodeproto

import "github.com/underpass-ai/underpass-bots/domain"

// RegisterRequest is what a node says about itself.
type RegisterRequest struct {
	ID           string                  `json:"id"`
	HostName     string                  `json:"hostName"`
	Labels       map[string]string       `json:"labels,omitempty"`
	Capabilities domain.NodeCapabilities `json:"capabilities"`
}
