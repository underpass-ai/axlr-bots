package nodeagent

import (
	"context"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// RenewCertificateBefore is how long before its certificate expires a
// node asks for a new one, unless Options.RenewBefore says otherwise.
// Thirty days is the panel's Soon, and leaves a month of daily tries to
// a node whose control plane is away.
const RenewCertificateBefore = 30 * 24 * time.Hour

// DefaultCertificateCheck is how often the agent looks at its
// certificate's expiry.
const DefaultCertificateCheck = 24 * time.Hour

// renewalTimeout bounds one renewal call: a half-open connection must
// not hold the check until the next day.
const renewalTimeout = time.Minute

// Certificate is the node's client certificate as the agent keeps it
// fresh. The keys, the X.509 and the files are the caller's: the agent
// only decides when, and asks the control plane.
type Certificate interface {
	// NotAfter is when the certificate in use expires.
	NotAfter() time.Time
	// NewRequest draws a new key and its signing request, both in PEM.
	NewRequest(node domain.NodeID) (keyPEM, csrPEM []byte, err error)
	// Install keeps the key with the renewed certificate and makes the
	// transport present them from then on.
	Install(keyPEM []byte, renewed application.RenewedCertificate) error
}

// RenewalDue reports whether a certificate that expires at notAfter is
// to be renewed now, renewing before its expiry (RenewCertificateBefore
// when not positive).
func RenewalDue(notAfter, now time.Time, before time.Duration) bool {
	if before <= 0 {
		before = RenewCertificateBefore
	}
	return notAfter.Sub(now) < before
}

// keepCertificate renews the node's certificate when it nears its
// expiry: at once, then every CertificateCheck, until ctx ends. A node
// over the socket has no certificate and keeps none.
func (a Agent) keepCertificate(ctx context.Context, id domain.NodeID) {
	if a.Certificate == nil {
		return
	}
	ticker := time.NewTicker(a.Options.certificateCheck())
	defer ticker.Stop()
	for {
		a.renewCertificateIfDue(ctx, id)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// renewCertificateIfDue renews the certificate when it is due; a
// failure is logged and tried again at the next check.
func (a Agent) renewCertificateIfDue(ctx context.Context, id domain.NodeID) {
	logger := a.Options.log()
	before := a.Certificate.NotAfter()
	if !RenewalDue(before, a.Options.now(), a.Options.RenewBefore) {
		return
	}
	key, csr, err := a.Certificate.NewRequest(id)
	if err != nil {
		logger.Printf("node: certificate renewal: %v (next try in %s)", err, a.Options.certificateCheck())
		return
	}
	// A stop does not cut the call short: the control plane may already
	// have denied the old certificate, and a node that drops the answer
	// keeps nothing that opens anything. The call is bounded on its own.
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), renewalTimeout)
	renewed, err := a.Transport.RenewCertificate(callCtx, id, csr)
	cancel()
	if err != nil {
		logger.Printf("node: certificate renewal: %v (next try in %s)", err, a.Options.certificateCheck())
		return
	}
	// The control plane already denies the old certificate: a new one
	// not kept leaves the node with none that opens anything.
	if err := a.Certificate.Install(key, renewed); err != nil {
		logger.Printf("node: certificate %s was issued but not kept: %v (the node needs a fresh enrollment token)", renewed.Serial, err)
		return
	}
	logger.Printf("node: certificate renewed: serial %s until %s (the old one ran until %s)", renewed.Serial, renewed.NotAfter.UTC().Format(time.RFC3339), before.UTC().Format(time.RFC3339))
}
