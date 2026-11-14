package grpcapi

import (
	"context"
	"crypto/x509"
)

// OperatorCertificates decides who a verified client certificate is: the
// operator's name, or an error (forbidden) when the certificate was not
// issued to an operator or was revoked. The pki adapter implements it.
type OperatorCertificates interface {
	Identify(ctx context.Context, cert *x509.Certificate) (string, error)
}
