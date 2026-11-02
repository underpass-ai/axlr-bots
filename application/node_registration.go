package application

import (
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// NodeRegistration is what a node agent says about itself when it
// registers: its id, its host and what it can run. Over mutual TLS the
// certificate it presented comes with it: its serial and expiry.
type NodeRegistration struct {
	ID                  string
	HostName            string
	Labels              map[string]string
	Capabilities        domain.NodeCapabilities
	CertificateSerial   string
	CertificateNotAfter time.Time
}
