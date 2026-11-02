package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// enrolled mints a token for web-2 and enrolls it; its certificate is
// serial-1.
func enrolled(t *testing.T, h *harness) {
	t.Helper()
	token := mint(t, h, "web-2", map[string]string{"zone": "eu"})
	if _, err := NewEnrollNode(h.deps).Execute(context.Background(), EnrollmentRequest{TokenID: token.ID.String(), Secret: token.Secret, HostName: "web-2.example", CSRPEM: []byte("CSR")}); err != nil {
		t.Fatal(err)
	}
	h.log.events, h.log.audits = nil, nil
}

func TestRenewNodeCertificateRotatesTheSerialAndDeniesTheOldOne(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	enrolled(t, h)
	h.clock.now = t0.Add(time.Hour)
	result, err := NewRenewNodeCertificate(h.deps).Execute(ctx, CertificateRenewal{NodeID: "web-2", Serial: "serial-1", CSRPEM: []byte("NEW CSR")})
	if err != nil {
		t.Fatal(err)
	}
	if result.Serial != "serial-2" || string(result.CertificatePEM) != "CERT web-2" || string(result.CAPEM) != "CA PEM" || result.NotAfter.IsZero() || string(h.ca.lastCSR) != "NEW CSR" || h.ca.lastID != "web-2" {
		t.Errorf("result = %+v", result)
	}
	stored, _ := h.nodes.Get(ctx, "web-2")
	if stored.CertificateSerial() != "serial-2" || !stored.CertificateExpiry().Equal(result.NotAfter) || stored.Labels()["zone"] != "eu" || stored.IsRevoked() {
		t.Errorf("node = %+v", stored.Snapshot())
	}
	if denied, _ := h.deny.IsDenied(ctx, "serial-1"); !denied {
		t.Error("the old serial is not denied")
	}
	if err := NewAuthenticateNode(h.deps).Execute(ctx, "web-2", "serial-1"); !domain.IsForbidden(err) {
		t.Errorf("the old certificate still opens: %v", err)
	}
	if err := NewAuthenticateNode(h.deps).Execute(ctx, "web-2", "serial-2"); err != nil {
		t.Errorf("the new certificate is refused: %v", err)
	}
	if got := h.log.eventTypes(); len(got) != 1 || got[0] != "node.renewed" || h.log.events[0].NodeID != "web-2" || h.log.events[0].Payload["previousSerial"] != "serial-1" || h.log.events[0].Payload["serial"] != "serial-2" {
		t.Errorf("events = %+v", h.log.events)
	}
	if len(h.log.audits) != 1 || h.log.audits[0].Action != "node.renew" || h.log.audits[0].Outcome != domain.AuditOK || h.log.audits[0].Details["previousSerial"] != "serial-1" || h.log.audits[0].Details["serial"] != "serial-2" {
		t.Errorf("audits = %+v", h.log.audits)
	}
	// The old certificate cannot renew again: it is no longer the node's.
	if _, err := NewRenewNodeCertificate(h.deps).Execute(ctx, CertificateRenewal{NodeID: "web-2", Serial: "serial-1", CSRPEM: []byte("CSR")}); !domain.IsForbidden(err) {
		t.Errorf("a superseded certificate renewed: %v", err)
	}
}

func TestRenewNodeCertificateRefusals(t *testing.T) {
	ctx := context.Background()
	// A revoked node is refused, stays revoked and gets no certificate.
	h := newHarness()
	enrolled(t, h)
	if _, err := NewRevokeNode(h.deps).Execute(ctx, "web-2"); err != nil {
		t.Fatal(err)
	}
	issued := h.ca.issued
	if _, err := NewRenewNodeCertificate(h.deps).Execute(ctx, CertificateRenewal{NodeID: "web-2", Serial: "serial-1", CSRPEM: []byte("CSR")}); !domain.IsForbidden(err) {
		t.Errorf("revoked: %v", err)
	}
	if stored, _ := h.nodes.Get(ctx, "web-2"); !stored.IsRevoked() || stored.CertificateSerial() != "serial-1" || h.ca.issued != issued {
		t.Errorf("revoked node = %+v, issued %d", stored.Snapshot(), h.ca.issued)
	}
	if last := h.log.audits[len(h.log.audits)-1]; last.Action != "node.renew" || last.Outcome != domain.AuditDenied {
		t.Errorf("audit = %+v", last)
	}
	// A revoke that lands while the certificate is signed wins.
	h = newHarness()
	enrolled(t, h)
	h.ca.onIssue = func() {
		h.ca.onIssue = nil
		if _, err := NewRevokeNode(h.deps).Execute(ctx, "web-2"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewRenewNodeCertificate(h.deps).Execute(ctx, CertificateRenewal{NodeID: "web-2", Serial: "serial-1", CSRPEM: []byte("CSR")}); !domain.IsForbidden(err) {
		t.Errorf("revoked meanwhile: %v", err)
	}
	if stored, _ := h.nodes.Get(ctx, "web-2"); !stored.IsRevoked() || stored.CertificateSerial() != "serial-1" {
		t.Errorf("revoked meanwhile: node = %+v", stored.Snapshot())
	}
	// A node the control plane does not know, a certificate that is not
	// the node's: refused, nothing signed.
	h = newHarness()
	enrolled(t, h)
	for name, request := range map[string]CertificateRenewal{
		"unknown node":   {NodeID: "web-9", Serial: "serial-1", CSRPEM: []byte("CSR")},
		"other serial":   {NodeID: "web-2", Serial: "serial-0", CSRPEM: []byte("CSR")},
		"no serial":      {NodeID: "web-2", CSRPEM: []byte("CSR")},
		"another's cert": {NodeID: "web-2", Serial: "serial-x", CSRPEM: []byte("CSR")},
	} {
		if _, err := NewRenewNodeCertificate(h.deps).Execute(ctx, request); !domain.IsForbidden(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if h.ca.issued != 1 {
		t.Errorf("issued = %d", h.ca.issued)
	}
	// The certifier refuses the request (a CSR naming another node, say):
	// the node keeps its certificate, the failure is audited.
	h.ca.fail = errBoom
	if _, err := NewRenewNodeCertificate(h.deps).Execute(ctx, CertificateRenewal{NodeID: "web-2", Serial: "serial-1", CSRPEM: []byte("CSR for web-9")}); !errors.Is(err, errBoom) {
		t.Errorf("certifier: %v", err)
	}
	if stored, _ := h.nodes.Get(ctx, "web-2"); stored.CertificateSerial() != "serial-1" {
		t.Errorf("certifier: node = %+v", stored.Snapshot())
	}
	if denied, _ := h.deny.IsDenied(ctx, "serial-1"); denied {
		t.Error("certifier: the current serial was denied")
	}
	if last := h.log.audits[len(h.log.audits)-1]; last.Outcome != domain.AuditFailed {
		t.Errorf("certifier: audit = %+v", last)
	}
	// Each port that fails fails the renewal.
	for name, breakIt := range map[string]func(h *harness){
		"ca":        func(h *harness) { h.ca.caFail = errBoom },
		"node save": func(h *harness) { h.nodes.fail = errBoom },
		"deny":      func(h *harness) { h.deny.fail = errBoom },
		"publish":   func(h *harness) { h.log.fail = errBoom },
		"audit":     func(h *harness) { h.log.recordFail = errBoom },
	} {
		h := newHarness()
		enrolled(t, h)
		breakIt(h)
		if _, err := NewRenewNodeCertificate(h.deps).Execute(ctx, CertificateRenewal{NodeID: "web-2", Serial: "serial-1", CSRPEM: []byte("CSR")}); !errors.Is(err, errBoom) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
