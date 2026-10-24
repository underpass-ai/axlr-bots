package application

import "github.com/underpass-ai/axlr-bots/domain"

// NodeRegistration is what a node agent says about itself when it
// registers: its id, its host and what it can run.
type NodeRegistration struct {
	ID           string
	HostName     string
	Labels       map[string]string
	Capabilities domain.NodeCapabilities
}
