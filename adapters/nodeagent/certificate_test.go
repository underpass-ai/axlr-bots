package nodeagent

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/axlr"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/internal/fakeaxlr"
)

// renewingTransport is a control plane that renews certificates and
// has no work: its poll waits for the agent to stop.
type renewingTransport struct {
	fakeTransport
	mu       sync.Mutex
	renewals int
	csrs     []string
	fail     error
	issue    time.Time
}

func (r *renewingTransport) NextAssignment(ctx context.Context, _ domain.NodeID, _ time.Duration) (application.Assignment, bool, error) {
	<-ctx.Done()
	return application.Assignment{}, false, ctx.Err()
}

func (r *renewingTransport) RenewCertificate(_ context.Context, _ domain.NodeID, csr []byte) (application.RenewedCertificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.renewals++
	r.csrs = append(r.csrs, string(csr))
	if r.fail != nil {
		return application.RenewedCertificate{}, r.fail
	}
	return application.RenewedCertificate{CertificatePEM: []byte("CERT"), CAPEM: []byte("CA"), Serial: "serial-2", NotAfter: r.issue}, nil
}

func (r *renewingTransport) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.renewals
}

// fakeCertificate is the node's certificate in memory.
type fakeCertificate struct {
	mu         sync.Mutex
	notAfter   time.Time
	requests   int
	installed  []string
	requestErr error
	installErr error
}

func (c *fakeCertificate) NotAfter() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.notAfter
}

func (c *fakeCertificate) NewRequest(node domain.NodeID) ([]byte, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.requestErr != nil {
		return nil, nil, c.requestErr
	}
	c.requests++
	return []byte("KEY"), []byte("CSR " + node.String()), nil
}

func (c *fakeCertificate) Install(key []byte, renewed application.RenewedCertificate) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.installErr != nil {
		return c.installErr
	}
	c.installed = append(c.installed, string(key)+"+"+renewed.Serial)
	c.notAfter = renewed.NotAfter
	return nil
}

func (c *fakeCertificate) installs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.installed...)
}

// renewingAgent is an agent whose clock stands at now, checking its
// certificate every few milliseconds.
func renewingAgent(t *testing.T, transport *renewingTransport, certificate *fakeCertificate, now time.Time, logs *syncBuffer) Agent {
	t.Helper()
	bin, _, err := fakeaxlr.Write(t.TempDir(), fakeaxlr.Options{Scenario: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	return Agent{
		Transport:   transport,
		Executor:    axlr.NewExecutor(bin, t.TempDir()),
		Certificate: certificate,
		Options:     Options{NodeID: "web-1", Heartbeat: time.Hour, CertificateCheck: 5 * time.Millisecond, Now: func() time.Time { return now }, Log: log.New(logs, "", 0)},
	}
}

// runFor runs the agent for a while and stops it.
func runFor(t *testing.T, agent Agent, d time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	if err := agent.Run(ctx); err != nil {
		t.Fatal(err)
	}
}

// Ten days before its certificate expires the agent renews it at start,
// once: the new one is far from expiry, so the checks after it renew
// nothing.
func TestAgentRenewsItsCertificateOnceWhenItNearsExpiry(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	transport := &renewingTransport{issue: now.Add(365 * 24 * time.Hour)}
	certificate := &fakeCertificate{notAfter: now.Add(10 * 24 * time.Hour)}
	logs := &syncBuffer{}
	runFor(t, renewingAgent(t, transport, certificate, now, logs), 300*time.Millisecond)
	if got := transport.count(); got != 1 {
		t.Errorf("renewals = %d, want 1", got)
	}
	if got := certificate.installs(); len(got) != 1 || got[0] != "KEY+serial-2" || transport.csrs[0] != "CSR web-1" {
		t.Errorf("installed = %v, requests %v", got, transport.csrs)
	}
	if !certificate.NotAfter().Equal(transport.issue) {
		t.Errorf("not after = %s", certificate.NotAfter())
	}
	if text := logs.String(); strings.Count(text, "certificate renewed: serial serial-2") != 1 {
		t.Errorf("log = %s", text)
	}
}

// A certificate far from its expiry is left alone.
func TestAgentLeavesACertificateFarFromExpiry(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	transport := &renewingTransport{issue: now.Add(365 * 24 * time.Hour)}
	certificate := &fakeCertificate{notAfter: now.Add(100 * 24 * time.Hour)}
	runFor(t, renewingAgent(t, transport, certificate, now, &syncBuffer{}), 100*time.Millisecond)
	if got := transport.count(); got != 0 || certificate.requests != 0 {
		t.Errorf("renewals = %d, requests %d", got, certificate.requests)
	}
	// An agent without a certificate (over the socket) keeps none.
	agent := Agent{Transport: transport}
	agent.keepCertificate(context.Background(), "web-1")
}

// A renewal that fails is tried again at the next check; a key that
// cannot be drawn asks nothing; a certificate issued but not kept says
// the node needs a token.
func TestAgentCertificateRenewalFailures(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	due := now.Add(24 * time.Hour)
	logs := &syncBuffer{}
	transport := &renewingTransport{fail: errors.New("control plane away"), issue: now.Add(365 * 24 * time.Hour)}
	certificate := &fakeCertificate{notAfter: due}
	runFor(t, renewingAgent(t, transport, certificate, now, logs), 100*time.Millisecond)
	if transport.count() < 2 || len(certificate.installs()) != 0 || !strings.Contains(logs.String(), "certificate renewal: control plane away (next try in 5ms)") {
		t.Errorf("renewals = %d, installed %v, log %s", transport.count(), certificate.installs(), logs.String())
	}
	agent := renewingAgent(t, &renewingTransport{issue: now.Add(365 * 24 * time.Hour)}, &fakeCertificate{notAfter: due, requestErr: errors.New("no entropy")}, now, logs)
	agent.renewCertificateIfDue(context.Background(), "web-1")
	if agent.Transport.(*renewingTransport).count() != 0 || !strings.Contains(logs.String(), "certificate renewal: no entropy") {
		t.Errorf("a key that cannot be drawn: %s", logs.String())
	}
	agent = renewingAgent(t, &renewingTransport{issue: now.Add(365 * 24 * time.Hour)}, &fakeCertificate{notAfter: due, installErr: errors.New("disk full")}, now, logs)
	agent.renewCertificateIfDue(context.Background(), "web-1")
	if !strings.Contains(logs.String(), "certificate serial-2 was issued but not kept: disk full (the node needs a fresh enrollment token)") {
		t.Errorf("not kept: %s", logs.String())
	}
}

func TestRenewalDue(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	for notAfter, want := range map[time.Time]bool{
		now.Add(-time.Hour):                           true,
		now.Add(10 * 24 * time.Hour):                  true,
		now.Add(RenewCertificateBefore - time.Second): true,
		now.Add(RenewCertificateBefore):               false,
		now.Add(365 * 24 * time.Hour):                 false,
	} {
		if got := RenewalDue(notAfter, now, 0); got != want {
			t.Errorf("RenewalDue(%s) = %v", notAfter, got)
		}
	}
	if !RenewalDue(now.Add(100*24*time.Hour), now, 365*24*time.Hour) {
		t.Error("a longer RenewBefore is not honoured")
	}
	if (Options{}).certificateCheck() != DefaultCertificateCheck || (Options{}).now().IsZero() {
		t.Error("defaults wrong")
	}
}
