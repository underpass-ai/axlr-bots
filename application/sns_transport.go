package application

import (
	"context"
	"crypto/x509"
)

// SNSTransport is Amazon SNS as a cloudwatch trigger reaches it: the
// certificate SNS signs a message with, fetched from its SigningCertURL
// (cached for a bounded time), and the visit of a SubscribeURL that
// confirms a subscription. domain.ParseSNSMessage has refused any other
// URL before one reaches it; it requests only what
// domain.SNSCertificateURL and domain.SNSSubscribeURL rebuild.
type SNSTransport interface {
	SigningCertificate(ctx context.Context, url string) (*x509.Certificate, error)
	ConfirmSubscription(ctx context.Context, url string) error
}
