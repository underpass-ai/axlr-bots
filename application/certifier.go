package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// Certifier is the fleet's certificate authority as the use cases see
// it: it signs a node's request into a certificate whose common name is
// the node id, and it says which CA nodes must trust. The keys and the
// X.509 details are the adapter's.
type Certifier interface {
	IssueNodeCertificate(ctx context.Context, node domain.NodeID, csrPEM []byte) (IssuedCertificate, error)
	// CACertificate is the CA certificate in PEM, what a token carries
	// and a node stores.
	CACertificate(ctx context.Context) ([]byte, error)
}
