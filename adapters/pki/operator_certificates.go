package pki

import (
	"context"
	"crypto/x509"
	"fmt"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// OperatorCertificates decides which verified client certificates are an
// operator's: issued to an operator (never to a node or a service) and
// not revoked. The chain is the TLS handshake's business; this is what
// the operator API adds.
type OperatorCertificates struct {
	denylist application.CertificateDenylist
}

// NewOperatorCertificates checks revocation against the denylist the
// node certificates share: serials are 128 random bits, so they cannot
// collide.
func NewOperatorCertificates(denylist application.CertificateDenylist) OperatorCertificates {
	return OperatorCertificates{denylist: denylist}
}

// Identify is the operator's name, or a ForbiddenError saying why the
// certificate does not serve as an operator's.
func (o OperatorCertificates) Identify(ctx context.Context, cert *x509.Certificate) (string, error) {
	name, ok := OperatorName(cert)
	if !ok {
		return "", domain.ForbiddenError{Reason: "the certificate was not issued to an operator"}
	}
	denied, err := o.denylist.IsDenied(ctx, SerialOf(cert))
	if err != nil {
		return "", fmt.Errorf("pki: revocation check: %w", err)
	}
	if denied {
		return "", domain.ForbiddenError{Reason: "the operator certificate was revoked", Revoked: true}
	}
	return name, nil
}
