package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// mint creates a token for the node and returns it.
func mint(t *testing.T, h *harness, nodeID string, labels map[string]string) EnrollmentToken {
	t.Helper()
	token, err := NewCreateEnrollmentToken(h.deps).Execute(context.Background(), nodeID, labels, 0)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestCreateEnrollmentToken(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	token := mint(t, h, "web-2", map[string]string{"zone": "eu"})
	if token.ID != "enr-1" || token.NodeID != "web-2" || token.Labels["zone"] != "eu" || len(token.Secret) < 40 || string(token.CAPEM) != "CA PEM" || !token.ExpiresAt.Equal(t0.Add(domain.DefaultEnrollmentTTL)) {
		t.Errorf("token = %+v", token)
	}
	stored, err := h.enrol.Get(ctx, token.ID)
	if err != nil || stored.Snapshot().SecretHash == token.Secret || stored.Snapshot().SecretHash != domain.HashEnrollmentSecret(token.Secret) || stored.IsUsed() {
		t.Errorf("stored = %+v %v", stored.Snapshot(), err)
	}
	if len(h.log.audits) != 1 || h.log.audits[0].Action != "node.enroll-token" || h.log.audits[0].Subject != "web-2" || h.log.audits[0].Details["token"] != "enr-1" || strings.Contains(h.log.audits[0].Details["expiresAt"], token.Secret) {
		t.Errorf("audits = %+v", h.log.audits)
	}
	// No node id: one is generated. A TTL is honoured.
	generated, err := NewCreateEnrollmentToken(h.deps).Execute(ctx, "", nil, 2*time.Hour)
	if err != nil || !strings.HasPrefix(generated.NodeID.String(), "node-") || !generated.ExpiresAt.Equal(t0.Add(2*time.Hour)) {
		t.Errorf("generated = %+v %v", generated, err)
	}
	if generated.Secret == token.Secret {
		t.Error("secrets repeat")
	}
	for name, tc := range map[string]struct {
		node    string
		labels  map[string]string
		ttl     time.Duration
		breakIt func(h *harness)
	}{
		"bad node id": {node: "Bad Id"},
		"bad label":   {node: "web-3", labels: map[string]string{"Bad": "x"}},
		"negative":    {node: "web-3", ttl: -time.Second},
		"ca":          {node: "web-3", breakIt: func(h *harness) { h.ca.caFail = errBoom }},
		"save":        {node: "web-3", breakIt: func(h *harness) { h.enrol.fail = errBoom }},
		"audit":       {node: "web-3", breakIt: func(h *harness) { h.log.recordFail = errBoom }},
	} {
		fresh := newHarness()
		if tc.breakIt != nil {
			tc.breakIt(fresh)
		}
		if _, err := NewCreateEnrollmentToken(fresh.deps).Execute(ctx, tc.node, tc.labels, tc.ttl); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestEnrollNodeIssuesOnceAndRecordsTheNode(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	token := mint(t, h, "web-2", map[string]string{"zone": "eu"})
	h.log.events, h.log.audits = nil, nil
	h.clock.now = t0.Add(time.Minute)
	result, err := NewEnrollNode(h.deps).Execute(ctx, EnrollmentRequest{TokenID: token.ID.String(), Secret: token.Secret, HostName: "web-2.example", CSRPEM: []byte("CSR")})
	if err != nil {
		t.Fatal(err)
	}
	if result.Node.ID() != "web-2" || result.Node.Status() != domain.NodeRegistered || result.Node.HostName() != "web-2.example" || result.Node.Labels()["zone"] != "eu" || result.Node.CertificateSerial() != "serial-1" {
		t.Errorf("node = %+v", result.Node.Snapshot())
	}
	if string(result.CertificatePEM) != "CERT web-2" || string(result.CAPEM) != "CA PEM" || result.Serial != "serial-1" || result.NotAfter.IsZero() || string(h.ca.lastCSR) != "CSR" || h.ca.lastID != "web-2" {
		t.Errorf("result = %+v", result)
	}
	stored, _ := h.nodes.Get(ctx, "web-2")
	if !stored.AcceptsCertificate("serial-1") {
		t.Error("the node does not accept its certificate")
	}
	used, _ := h.enrol.Get(ctx, token.ID)
	if !used.IsUsed() {
		t.Error("the token was not consumed")
	}
	if got := h.log.eventTypes(); len(got) != 1 || got[0] != "node.enrolled" || h.log.events[0].NodeID != "web-2" || h.log.events[0].Payload["serial"] != "serial-1" || h.log.events[0].Payload["token"] != "enr-1" {
		t.Errorf("events = %v %+v", got, h.log.events)
	}
	if len(h.log.audits) != 1 || h.log.audits[0].Action != "node.enroll" || h.log.audits[0].Outcome != domain.AuditOK || h.log.audits[0].Details["serial"] != "serial-1" {
		t.Errorf("audits = %+v", h.log.audits)
	}
	// The same token again is refused, and the refusal is audited.
	h.log.audits = nil
	var state domain.StateError
	if _, err := NewEnrollNode(h.deps).Execute(ctx, EnrollmentRequest{TokenID: token.ID.String(), Secret: token.Secret, CSRPEM: []byte("CSR")}); !errors.As(err, &state) || !strings.Contains(err.Error(), "used at") {
		t.Errorf("reuse: %v", err)
	}
	if len(h.log.audits) != 1 || h.log.audits[0].Outcome != domain.AuditDenied || h.log.audits[0].Details["token"] != "enr-1" {
		t.Errorf("denied audit = %+v", h.log.audits)
	}
	if h.ca.issued != 1 {
		t.Errorf("issued %d certificates", h.ca.issued)
	}
	// Enrolling again with a fresh token rotates the certificate and
	// keeps the host name the agent reported; the old serial is no
	// longer accepted.
	second := mint(t, h, "web-2", nil)
	rotated, err := NewEnrollNode(h.deps).Execute(ctx, EnrollmentRequest{TokenID: second.ID.String(), Secret: second.Secret, CSRPEM: []byte("CSR2")})
	if err != nil || rotated.Serial != "serial-2" || rotated.Node.Labels()["zone"] != "eu" || rotated.Node.HostName() != "web-2.example" {
		t.Errorf("rotate: %v %+v", err, rotated.Node.Snapshot())
	}
	if node, _ := h.nodes.Get(ctx, "web-2"); node.AcceptsCertificate("serial-1") || !node.AcceptsCertificate("serial-2") {
		t.Error("the old certificate is still accepted")
	}
	if denied, _ := h.deny.IsDenied(ctx, "serial-1"); !denied {
		t.Error("the superseded serial is not on the denylist")
	}
	if denied, _ := h.deny.IsDenied(ctx, "serial-2"); denied {
		t.Error("the current serial is on the denylist")
	}
	// The host name defaults to the node id.
	third := mint(t, h, "db-1", nil)
	created, err := NewEnrollNode(h.deps).Execute(ctx, EnrollmentRequest{TokenID: third.ID.String(), Secret: third.Secret, CSRPEM: []byte("CSR3")})
	if err != nil || created.Node.HostName() != "db-1" {
		t.Errorf("default host: %v %+v", err, created.Node.Snapshot())
	}
}

func TestEnrollNodeRefusals(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	token := mint(t, h, "web-2", nil)
	if _, err := NewEnrollNode(h.deps).Execute(ctx, EnrollmentRequest{TokenID: "Bad Id"}); err == nil {
		t.Error("bad token id accepted")
	}
	if _, err := NewEnrollNode(h.deps).Execute(ctx, EnrollmentRequest{TokenID: "enr-404", Secret: token.Secret}); !domain.IsForbidden(err) {
		t.Errorf("unknown token: %v", err)
	}
	if _, err := NewEnrollNode(h.deps).Execute(ctx, EnrollmentRequest{TokenID: token.ID.String(), Secret: "wrong-secret-wrong-secret"}); !domain.IsForbidden(err) {
		t.Errorf("wrong secret: %v", err)
	}
	if len(h.log.audits) != 2 || h.log.audits[1].Outcome != domain.AuditDenied {
		t.Errorf("audits = %+v", h.log.audits)
	}
	// Expired.
	h.clock.now = t0.Add(2 * domain.DefaultEnrollmentTTL)
	var state domain.StateError
	if _, err := NewEnrollNode(h.deps).Execute(ctx, EnrollmentRequest{TokenID: token.ID.String(), Secret: token.Secret}); !errors.As(err, &state) || !strings.Contains(err.Error(), "expired") {
		t.Errorf("expired: %v", err)
	}
	h.clock.now = t0
	// The certifier refuses: the token is burnt, the failure audited.
	h.ca.fail = errBoom
	if _, err := NewEnrollNode(h.deps).Execute(ctx, EnrollmentRequest{TokenID: token.ID.String(), Secret: token.Secret}); !errors.Is(err, errBoom) {
		t.Errorf("certifier: %v", err)
	}
	if used, _ := h.enrol.Get(ctx, token.ID); !used.IsUsed() {
		t.Error("the token survived a failed issue")
	}
	if last := h.log.audits[len(h.log.audits)-1]; last.Outcome != domain.AuditFailed {
		t.Errorf("failed audit = %+v", last)
	}
	for name, breakIt := range map[string]func(h *harness){
		"enrollment get":  func(h *harness) { h.enrol.listFail = errBoom },
		"enrollment save": func(h *harness) { h.enrol.fail = errBoom },
		"ca":              func(h *harness) { h.ca.caFail = errBoom },
		"node save":       func(h *harness) { h.nodes.fail = errBoom },
		"publish":         func(h *harness) { h.log.fail = errBoom },
		"audit":           func(h *harness) { h.log.recordFail = errBoom },
		"deny the previous": func(h *harness) {
			first := mint(t, h, "web-2", nil)
			if _, err := NewEnrollNode(h.deps).Execute(ctx, EnrollmentRequest{TokenID: first.ID.String(), Secret: first.Secret, CSRPEM: []byte("CSR")}); err != nil {
				t.Fatal(err)
			}
			h.deny.fail = errBoom
		},
	} {
		h := newHarness()
		token := mint(t, h, "web-2", nil)
		breakIt(h)
		if _, err := NewEnrollNode(h.deps).Execute(ctx, EnrollmentRequest{TokenID: token.ID.String(), Secret: token.Secret, CSRPEM: []byte("CSR")}); !errors.Is(err, errBoom) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A revoked node is readmitted by a fresh token.
	h = newHarness()
	token = mint(t, h, "web-2", nil)
	if _, err := NewEnrollNode(h.deps).Execute(ctx, EnrollmentRequest{TokenID: token.ID.String(), Secret: token.Secret, CSRPEM: []byte("CSR")}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRevokeNode(h.deps).Execute(ctx, "web-2"); err != nil {
		t.Fatal(err)
	}
	again := mint(t, h, "web-2", nil)
	readmitted, err := NewEnrollNode(h.deps).Execute(ctx, EnrollmentRequest{TokenID: again.ID.String(), Secret: again.Secret, CSRPEM: []byte("CSR")})
	if err != nil || readmitted.Node.IsRevoked() || readmitted.Node.Status() != domain.NodeRegistered || readmitted.Serial != "serial-2" {
		t.Errorf("readmit: %v %+v", err, readmitted.Node.Snapshot())
	}
	if err := NewAuthenticateNode(h.deps).Execute(ctx, "web-2", "serial-1"); !domain.IsForbidden(err) {
		t.Errorf("the revoked serial is accepted after readmission: %v", err)
	}
	if err := NewAuthenticateNode(h.deps).Execute(ctx, "web-2", "serial-2"); err != nil {
		t.Errorf("the new serial is refused: %v", err)
	}
}

func TestRevokeNodeFailsItsExecutionsAndDeniesItsCertificate(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	token := mint(t, h, "web-1", nil)
	if _, err := NewEnrollNode(h.deps).Execute(ctx, EnrollmentRequest{TokenID: token.ID.String(), Secret: token.Secret, CSRPEM: []byte("CSR")}); err != nil {
		t.Fatal(err)
	}
	onlineNode(t, h, "web-1")
	task := submit(t, h, bot, "k-1").Task
	assignment, found, err := NewNextAssignment(h.deps).Execute(ctx, "web-1")
	if err != nil || !found {
		t.Fatalf("assignment: %v %v", err, found)
	}
	other := submit(t, h, bot, "k-2").Task
	h.log.events, h.log.audits = nil, nil
	h.clock.now = t0.Add(time.Minute)
	result, err := NewRevokeNode(h.deps).Execute(ctx, "web-1")
	if err != nil || !result.Node.IsRevoked() || len(result.FailedTasks) != 1 || result.FailedTasks[0] != task.ID() {
		t.Fatalf("revoke: %v %+v", err, result)
	}
	if denied, _ := h.deny.IsDenied(ctx, "serial-1"); !denied {
		t.Error("the serial is not denied")
	}
	failed, _ := h.tasks.Get(ctx, task.ID())
	execution, _ := h.execs.Get(ctx, assignment.ExecutionID)
	if failed.Status() != domain.TaskFailed || !strings.Contains(failed.FailureReason(), "revoked: node web-1") || execution.Outcome() != domain.ExecutionFailed || !execution.EndedAt().Equal(h.clock.now) {
		t.Errorf("task = %+v execution = %+v", failed.Snapshot(), execution.Snapshot())
	}
	if pending, _ := h.tasks.Get(ctx, other.ID()); pending.Status() != domain.TaskPending {
		t.Error("a pending task was failed")
	}
	if got := h.log.eventTypes(); len(got) != 2 || got[0] != "bot.task.failed" || got[1] != "node.revoked" || h.log.events[0].Payload["reason"] != "revoked" {
		t.Errorf("events = %v", got)
	}
	if len(h.log.audits) != 1 || h.log.audits[0].Action != "node.revoke" || h.log.audits[0].Details["failedTasks"] != task.ID().String() || h.log.audits[0].Details["serial"] != "serial-1" {
		t.Errorf("audits = %+v", h.log.audits)
	}
	if err := NewAuthenticateNode(h.deps).Execute(ctx, "web-1", "serial-1"); !domain.IsForbidden(err) || !strings.Contains(err.Error(), "revoked") {
		t.Errorf("authenticate revoked: %v", err)
	}
	if _, err := NewHeartbeatNode(h.deps).Execute(ctx, "web-1"); err == nil {
		t.Error("a revoked node heartbeat")
	}
	if _, err := NewRegisterNode(h.deps).Execute(ctx, registration("web-1")); err == nil {
		t.Error("a revoked node registered")
	}
	if _, _, err := NewNextAssignment(h.deps).Execute(ctx, "web-1"); err == nil {
		t.Error("a revoked node took work")
	}
	if offline, err := NewMarkOfflineNodes(h.deps).Execute(ctx, time.Nanosecond); err != nil || len(offline) != 0 {
		t.Errorf("a revoked node was marked offline: %v %v", offline, err)
	}
	var state domain.StateError
	if _, err := NewRevokeNode(h.deps).Execute(ctx, "web-1"); !errors.As(err, &state) {
		t.Errorf("revoke twice: %v", err)
	}
	if _, err := NewRevokeNode(h.deps).Execute(ctx, "ghost"); !domain.IsNotFound(err) {
		t.Errorf("unknown: %v", err)
	}
	// A socket node, never enrolled, has no serial to deny.
	onlineNode(t, h, "local")
	if res, err := NewRevokeNode(h.deps).Execute(ctx, "local"); err != nil || res.Node.CertificateSerial() != "" || len(h.deny.serials) != 1 {
		t.Errorf("revoke a socket node: %v", err)
	}
	for name, breakIt := range map[string]func(h *harness){
		"deny":           func(h *harness) { h.deny.fail = errBoom },
		"node save":      func(h *harness) { h.nodes.fail = errBoom },
		"list running":   func(h *harness) { h.execs.listFail = errBoom },
		"execution save": func(h *harness) { h.execs.fail = errBoom },
		"task save":      func(h *harness) { h.tasks.fail = errBoom },
		"publish":        func(h *harness) { h.log.fail = errBoom },
		"audit":          func(h *harness) { h.log.recordFail = errBoom },
	} {
		h := newHarness()
		bot := enabledBot(t, h)
		token := mint(t, h, "web-1", nil)
		_, _ = NewEnrollNode(h.deps).Execute(ctx, EnrollmentRequest{TokenID: token.ID.String(), Secret: token.Secret, CSRPEM: []byte("CSR")})
		onlineNode(t, h, "web-1")
		submit(t, h, bot, "k-1")
		if _, _, err := NewNextAssignment(h.deps).Execute(ctx, "web-1"); err != nil {
			t.Fatal(err)
		}
		breakIt(h)
		if _, err := NewRevokeNode(h.deps).Execute(ctx, "web-1"); !errors.Is(err, errBoom) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAuthenticateNode(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	auth := NewAuthenticateNode(h.deps)
	if err := auth.Execute(ctx, "web-1", ""); !domain.IsForbidden(err) {
		t.Errorf("no certificate: %v", err)
	}
	// Unknown to the control plane but signed by the CA: may register.
	if err := auth.Execute(ctx, "web-1", "serial-9"); err != nil {
		t.Errorf("unknown node: %v", err)
	}
	token := mint(t, h, "web-1", nil)
	if _, err := NewEnrollNode(h.deps).Execute(ctx, EnrollmentRequest{TokenID: token.ID.String(), Secret: token.Secret, CSRPEM: []byte("CSR")}); err != nil {
		t.Fatal(err)
	}
	if err := auth.Execute(ctx, "web-1", "serial-1"); err != nil {
		t.Errorf("current: %v", err)
	}
	if err := auth.Execute(ctx, "web-1", "serial-9"); !domain.IsForbidden(err) {
		t.Errorf("another serial: %v", err)
	}
	_ = h.deny.Deny(ctx, "serial-1", "web-1")
	if err := auth.Execute(ctx, "web-1", "serial-1"); !domain.IsForbidden(err) {
		t.Errorf("denied: %v", err)
	}
	h.deny.fail = errBoom
	if err := auth.Execute(ctx, "web-1", "serial-1"); !errors.Is(err, errBoom) {
		t.Errorf("denylist error: %v", err)
	}
}
