package sns

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/internal/snstest"
)

const certURL = "https://sns.eu-west-1.amazonaws.com/SimpleNotificationService-1a2b.pem"

// served is a TLS server that answers for sns.<region>.amazonaws.com: the
// client dials it whatever the host, and verifies its certificate as
// example.com (httptest's), so the host check stays the real one.
func served(t *testing.T, handler http.HandlerFunc) (*http.Client, *int32) {
	t.Helper()
	var hits int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	client := server.Client()
	transport := client.Transport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	transport.TLSClientConfig = &tls.Config{RootCAs: transport.TLSClientConfig.RootCAs, ServerName: "example.com", MinVersion: tls.VersionTLS12}
	client.Transport = transport
	return client, &hits
}

// hostileURLs are refused before any request; each is well shaped but
// for the one thing it tries.
var hostileURLs = []string{
	"http://sns.eu-west-1.amazonaws.com/SimpleNotificationService-1a2b.pem",
	"https://sns.eu-west-1.amazonaws.com.evil.example/SimpleNotificationService-1a2b.pem",
	"https://evil.example/SimpleNotificationService-1a2b.pem",
	"https://sns.eu-west-1.amazonaws.com@evil.example/SimpleNotificationService-1a2b.pem",
	"https://user:pass@sns.eu-west-1.amazonaws.com/SimpleNotificationService-1a2b.pem",
	"https://sns.eu-west-1.amazonaws.com:444/SimpleNotificationService-1a2b.pem",
	"https://169.254.169.254/SimpleNotificationService-1a2b.pem",
	"https://127.0.0.1/SimpleNotificationService-1a2b.pem",
	"https://[::1]/SimpleNotificationService-1a2b.pem",
	"https://sns.eu-west-1.amazonaws.com/latest/meta-data/",
	"https://sns.eu-west-1.amazonaws.com/SimpleNotificationService-1a2b.pem?x=1",
	"https://169.254.169.254/?Action=ConfirmSubscription&TopicArn=arn:aws:sns:eu-west-1:123456789012:t&Token=abc",
}

func TestSigningCertificateIsFetchedAndCached(t *testing.T) {
	now := time.Now()
	signer, err := snstest.NewSigner(now)
	if err != nil {
		t.Fatal(err)
	}
	client, hits := served(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/SimpleNotificationService-1a2b.pem" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(signer.PEM)
	})
	clock := now
	transport := NewTransportWith(client, time.Hour, func() time.Time { return clock })
	ctx := context.Background()
	certificate, err := transport.SigningCertificate(ctx, certURL)
	if err != nil || !certificate.Equal(signer.Certificate) {
		t.Fatalf("certificate = %v %v", certificate, err)
	}
	if _, err := transport.SigningCertificate(ctx, certURL); err != nil || atomic.LoadInt32(hits) != 1 {
		t.Errorf("cached: %v, %d fetches", err, *hits)
	}
	clock = clock.Add(61 * time.Minute)
	if _, err := transport.SigningCertificate(ctx, certURL); err != nil || atomic.LoadInt32(hits) != 2 {
		t.Errorf("after the TTL: %v, %d fetches", err, *hits)
	}
	// Past the certificate's own expiry it is refused.
	clock = now.Add(13 * time.Hour)
	if _, err := transport.SigningCertificate(ctx, certURL); err == nil || !strings.Contains(err.Error(), "validity") {
		t.Errorf("expired: %v", err)
	}
	if _, err := transport.SigningCertificate(ctx, "https://sns.eu-west-1.amazonaws.com/SimpleNotificationService-missing.pem"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("missing: %v", err)
	}
}

func TestSigningCertificateRefusals(t *testing.T) {
	answer := "not a certificate"
	client, hits := served(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/SimpleNotificationService-redirect.pem":
			http.Redirect(w, r, "https://evil.example/cert.pem", http.StatusFound)
		case "/SimpleNotificationService-large.pem":
			_, _ = w.Write([]byte(strings.Repeat("x", maxBodyBytes+10)))
		default:
			_, _ = w.Write([]byte(answer))
		}
	})
	transport := NewTransportWith(client, time.Hour, time.Now)
	ctx := context.Background()
	for _, url := range hostileURLs {
		if _, err := transport.SigningCertificate(ctx, url); err == nil {
			t.Errorf("%s fetched", url)
		}
		if err := transport.ConfirmSubscription(ctx, url); err == nil {
			t.Errorf("%s confirmed", url)
		}
	}
	if atomic.LoadInt32(hits) != 0 {
		t.Errorf("a refused URL was fetched: %d", *hits)
	}
	if _, err := transport.SigningCertificate(ctx, "https://sns.eu-west-1.amazonaws.com/SimpleNotificationService-redirect.pem"); err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Errorf("redirect: %v", err)
	}
	if _, err := transport.SigningCertificate(ctx, "https://sns.eu-west-1.amazonaws.com/SimpleNotificationService-large.pem"); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("large: %v", err)
	}
	if _, err := transport.SigningCertificate(ctx, certURL); err == nil || !strings.Contains(err.Error(), "PEM") {
		t.Errorf("not PEM: %v", err)
	}
	answer = "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"
	if _, err := transport.SigningCertificate(ctx, certURL); err == nil {
		t.Error("a broken certificate parsed")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := transport.SigningCertificate(canceled, certURL); err == nil {
		t.Error("a canceled fetch succeeded")
	}
	if NewTransport() == nil {
		t.Error("no default transport")
	}
}

func TestConfirmSubscription(t *testing.T) {
	var query string
	client, _ := served(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		if r.URL.Query().Get("Token") == "bad" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("<ConfirmSubscriptionResponse/>"))
	})
	transport := NewTransportWith(client, time.Hour, time.Now)
	ctx := context.Background()
	if err := transport.ConfirmSubscription(ctx, "https://sns.eu-west-1.amazonaws.com/?Action=ConfirmSubscription&TopicArn=arn:aws:sns:eu-west-1:123456789012:t&Token=abc"); err != nil || !strings.Contains(query, "Token=abc") {
		t.Errorf("confirm: %v %q", err, query)
	}
	if err := transport.ConfirmSubscription(ctx, "https://sns.eu-west-1.amazonaws.com/?Action=ConfirmSubscription&TopicArn=arn:aws:sns:eu-west-1:123456789012:t&Token=bad"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("refused: %v", err)
	}
}

func TestCacheStaysBounded(t *testing.T) {
	now := time.Now()
	signer, err := snstest.NewSigner(now)
	if err != nil {
		t.Fatal(err)
	}
	client, _ := served(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(signer.PEM) })
	clock := now
	transport := NewTransportWith(client, time.Hour, func() time.Time { return clock })
	for n := 0; n < maxCached+5; n++ {
		clock = clock.Add(time.Second)
		if _, err := transport.SigningCertificate(context.Background(), "https://sns.eu-west-1.amazonaws.com/SimpleNotificationService-c"+strings.Repeat("x", n)+".pem"); err != nil {
			t.Fatal(err)
		}
	}
	if len(transport.cache) != maxCached {
		t.Errorf("cache = %d", len(transport.cache))
	}
	// Once they expire, the next store drops them.
	clock = clock.Add(2 * time.Hour)
	transport.store("https://sns.eu-west-1.amazonaws.com/fresh.pem", entry{certificate: signer.Certificate, expires: clock.Add(time.Hour)}, clock)
	if len(transport.cache) != 1 {
		t.Errorf("cache after expiry = %d", len(transport.cache))
	}
}
