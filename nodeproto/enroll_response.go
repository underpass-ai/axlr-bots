package nodeproto

import (
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// EnrollResponse is what an enrolled node receives: its certificate and
// the CA in PEM, the certificate's serial and expiry, and the node as
// the control plane now knows it (its id and the labels it starts with).
type EnrollResponse struct {
	Node        domain.NodeSnapshot `json:"node"`
	Certificate string              `json:"certificate"`
	CA          string              `json:"ca"`
	Serial      string              `json:"serial"`
	NotAfter    time.Time           `json:"notAfter"`
}
