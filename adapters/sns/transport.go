// Package sns is Amazon SNS as a cloudwatch trigger reaches it: the
// signing certificate fetched from a message's SigningCertURL and kept
// for a bounded time, and the visit of a SubscribeURL that confirms a
// subscription. Every URL must be on sns.<region>.amazonaws.com over
// HTTPS (domain.SNSURLAllowed) and no redirect is followed, so a forged
// message cannot make the control plane fetch an address of its choosing.
package sns

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// Bounds of the transport.
const (
	// CertificateTTL is how long a fetched certificate is used before it
	// is fetched again; never past the certificate's own expiry.
	CertificateTTL = time.Hour
	// maxCached is how many certificates are kept: SNS signs with one per
	// region at a time, and rotates it rarely.
	maxCached = 32
	// maxBodyBytes bounds what a certificate or a confirmation may answer.
	maxBodyBytes = 64 << 10
	// requestTimeout bounds one fetch.
	requestTimeout = 10 * time.Second
)

// errRedirect refuses a redirect: SNS answers its own URLs directly.
var errRedirect = errors.New("sns: a redirect is not followed")

type entry struct {
	certificate *x509.Certificate
	expires     time.Time
}

// Transport fetches over HTTPS and caches the certificates.
type Transport struct {
	client *http.Client
	ttl    time.Duration
	now    func() time.Time
	mu     sync.Mutex
	cache  map[string]entry
}

// NewTransport is the transport the daemon uses: the system's roots, a
// ten-second timeout, an hour's cache, no redirects.
func NewTransport() *Transport {
	return NewTransportWith(&http.Client{Timeout: requestTimeout}, CertificateTTL, time.Now)
}

// NewTransportWith is the transport over the client given (its redirects
// refused), with the cache's lifetime and clock: what the tests use.
func NewTransportWith(client *http.Client, ttl time.Duration, now func() time.Time) *Transport {
	copied := *client
	copied.CheckRedirect = func(*http.Request, []*http.Request) error { return errRedirect }
	return &Transport{client: &copied, ttl: ttl, now: now, cache: map[string]entry{}}
}

// SigningCertificate returns the certificate at the URL, from the cache
// while it is fresh. A certificate outside its validity is refused.
func (t *Transport) SigningCertificate(ctx context.Context, url string) (*x509.Certificate, error) {
	if !domain.SNSURLAllowed(url) {
		return nil, fmt.Errorf("sns: %q is not on sns.<region>.amazonaws.com over HTTPS", url)
	}
	now := t.now()
	t.mu.Lock()
	cached, ok := t.cache[url]
	t.mu.Unlock()
	if ok && now.Before(cached.expires) {
		return cached.certificate, nil
	}
	body, err := t.get(ctx, url)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(body)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("sns: the signing certificate is not a PEM certificate")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("sns: the signing certificate: %w", err)
	}
	if now.Before(certificate.NotBefore) || now.After(certificate.NotAfter) {
		return nil, errors.New("sns: the signing certificate is outside its validity")
	}
	expires := now.Add(t.ttl)
	if certificate.NotAfter.Before(expires) {
		expires = certificate.NotAfter
	}
	t.store(url, entry{certificate: certificate, expires: expires}, now)
	return certificate, nil
}

// store keeps the certificate, dropping the expired ones and, when the
// cache is still full, the one that expires first.
func (t *Transport) store(url string, e entry, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for key, cached := range t.cache {
		if !now.Before(cached.expires) {
			delete(t.cache, key)
		}
	}
	if _, ok := t.cache[url]; !ok && len(t.cache) >= maxCached {
		var oldest string
		for key, cached := range t.cache {
			if oldest == "" || cached.expires.Before(t.cache[oldest].expires) {
				oldest = key
			}
		}
		delete(t.cache, oldest)
	}
	t.cache[url] = e
}

// ConfirmSubscription visits the SubscribeURL: SNS confirms the
// subscription when it answers 200.
func (t *Transport) ConfirmSubscription(ctx context.Context, url string) error {
	if !domain.SNSURLAllowed(url) {
		return fmt.Errorf("sns: %q is not on sns.<region>.amazonaws.com over HTTPS", url)
	}
	_, err := t.get(ctx, url)
	return err
}

func (t *Transport) get(ctx context.Context, url string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("sns: %w", err)
	}
	response, err := t.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("sns: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("sns: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sns: %s answered %d", request.URL.Host, response.StatusCode)
	}
	if len(body) > maxBodyBytes {
		return nil, errors.New("sns: the answer is too large")
	}
	return body, nil
}
