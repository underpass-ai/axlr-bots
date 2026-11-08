package application

import (
	"context"
	"crypto/x509"
)

// SNSTransport is Amazon SNS as a cloudwatch trigger reaches it: the
// certificate SNS signs a message with, fetched from its SigningCertURL
// (cached for a bounded time), and the visit of a SubscribeURL that
// confirms a subscription. Both URLs are checked against
// domain.SNSURLAllowed before they reach it; it checks them again.
type SNSTransport interface {
	SigningCertificate(ctx context.Context, url string) (*x509.Certificate, error)
	ConfirmSubscription(ctx context.Context, url string) error
}
