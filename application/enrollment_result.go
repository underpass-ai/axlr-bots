package application

import (
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// EnrollmentResult is what an enrolled node receives: its certificate,
// the CA to trust, and the node as the control plane now knows it.
type EnrollmentResult struct {
	Node           domain.Node
	CertificatePEM []byte
	CAPEM          []byte
	Serial         string
	NotAfter       time.Time
}
