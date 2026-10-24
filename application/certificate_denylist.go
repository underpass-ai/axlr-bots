package application

import "context"

// CertificateDenylist keeps the serials of revoked node certificates,
// checked on every request a certificate authenticates. It outlives the
// node's record: a deleted node file does not readmit its certificate.
type CertificateDenylist interface {
	Deny(ctx context.Context, serial string, node string) error
	IsDenied(ctx context.Context, serial string) (bool, error)
}
