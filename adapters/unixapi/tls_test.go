package unixapi

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/pki"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/compose"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/nodeproto"
)

// fleet is a control plane with a CA, serving the socket and a TLS
// address on the loopback.
type fleet struct {
	operator *Client
	url      string
	ca       pki.Authority
	other    pki.Authority
}

// serveFleet starts the control plane over the socket and TLS.
func serveFleet(t *testing.T) *fleet {
	t.Helper()
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	deps, err := compose.Dependencies(state, "/nonexistent/axlr-tui", "test:api")
	if err != nil {
		t.Fatal(err)
	}
	ca, err := pki.Generate(filepath.Join(state, pki.CADirName))
	if err != nil {
		t.Fatal(err)
	}
	other, _ := pki.Generate(filepath.Join(dir, "other"))
	local := NewLocal(deps)
	server := NewServer(local)
	socket := filepath.Join(dir, "cp.sock")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 2)
	go func() { done <- server.Serve(ctx, socket) }()
	go func() { done <- server.ServeTLS(ctx, listener, pki.ServerTLSConfig(ca, []string{"127.0.0.1"})) }()
	t.Cleanup(func() {
		cancel()
		for range 2 {
			if err := <-done; err != nil {
				t.Error(err)
			}
		}
	})
	for !Reachable(context.Background(), socket) {
		time.Sleep(10 * time.Millisecond)
	}
	return &fleet{operator: NewClient(socket), url: "https://" + listener.Addr().String(), ca: ca, other: other}
}

// token mints a token for the node through the operator's socket.
func (f *fleet) token(t *testing.T, nodeID string, labels map[string]string) nodeproto.EnrollToken {
	t.Helper()
	response, err := f.operator.CreateEnrollmentToken(context.Background(), EnrollTokenRequest{NodeID: nodeID, Labels: labels})
	if err != nil {
		t.Fatal(err)
	}
	if response.NodeID != nodeID || response.ExpiresAt.Before(time.Now()) || !strings.HasPrefix(response.Token, "axlr1.") {
		t.Fatalf("token = %+v", response)
	}
	token, err := nodeproto.DecodeEnrollToken(response.Token)
	if err != nil || token.NodeID != nodeID || token.CAPEM != string(f.ca.CertificatePEM()) {
		t.Fatalf("decode: %v %+v", err, token)
	}
	return token
}

// enroll does what axlr-node enroll does: a key, a request, the token
// over TLS trusting the CA it carries, and the credentials back.
func (f *fleet) enroll(t *testing.T, token nodeproto.EnrollToken) (pki.Credentials, *Client) {
	t.Helper()
	key, csr, err := pki.NewNodeKeyAndRequest(domain.NodeID(token.NodeID))
	if err != nil {
		t.Fatal(err)
	}
	config, err := pki.EnrollTLSConfig([]byte(token.CAPEM))
	if err != nil {
		t.Fatal(err)
	}
	response, err := NewTLSClient(f.url, config).Enroll(context.Background(), nodeproto.EnrollRequest{TokenID: token.ID, Secret: token.Secret, HostName: token.NodeID + ".example", CSR: string(csr)})
	if err != nil {
		t.Fatal(err)
	}
	creds, err := pki.ParseCredentials(key, []byte(response.Certificate), []byte(response.CA))
	if err != nil || creds.NodeID != token.NodeID || creds.Serial != response.Serial || response.Node.CertificateSerial != response.Serial || response.Node.Status == "Revoked" {
		t.Fatalf("credentials: %v %+v", err, response.Node)
	}
	clientConfig, _ := creds.ClientTLSConfig()
	return creds, NewTLSClient(f.url, clientConfig)
}

func TestEnrolledNodesSpeakOverTLS(t *testing.T) {
	f := serveFleet(t)
	ctx := context.Background()
	token := f.token(t, "web-1", map[string]string{"zone": "eu"})
	creds, node := f.enroll(t, token)
	// Register without an id in the body: the certificate names the node.
	registered, err := node.Register(ctx, application.NodeRegistration{HostName: "web-1.example", Labels: map[string]string{"zone": "eu", "host": "web-1"}})
	if err != nil || registered.ID != "web-1" || registered.Status != "Online" || registered.CertificateSerial != creds.Serial || registered.Labels["zone"] != "eu" {
		t.Fatalf("register: %v %+v", err, registered)
	}
	if beat, err := node.Heartbeat(ctx, "web-1"); err != nil || beat.Status != "Online" {
		t.Errorf("heartbeat: %v %+v", err, beat)
	}
	if _, found, err := node.NextAssignment(ctx, "web-1", 50*time.Millisecond); err != nil || found {
		t.Errorf("poll: %v %v", err, found)
	}
	listed, err := f.operator.ListNodes(ctx)
	if err != nil || len(listed) != 1 || listed[0].CertificateSerial != creds.Serial {
		t.Errorf("list: %v %+v", err, listed)
	}
	// The whole flow: a task assigned, leased and reported over TLS.
	if _, err := f.operator.RegisterBot(ctx, definition("bot-a", "observe")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.operator.EnableBot(ctx, "bot-a"); err != nil {
		t.Fatal(err)
	}
	submitted, _ := f.operator.SubmitTask(ctx, SubmitRequest{BotID: "bot-a", Objective: "list", IdempotencyKey: "k-1"})
	assignment, found, err := node.NextAssignment(ctx, "web-1", 5*time.Second)
	if err != nil || !found || assignment.Run.TaskID != domain.TaskID(submitted.Task.ID) {
		t.Fatalf("assignment: %v %v", err, found)
	}
	if _, err := node.RenewLease(ctx, "web-1", assignment.ExecutionID); err != nil {
		t.Error(err)
	}
	ack, err := node.Report(ctx, "web-1", assignment.ExecutionID, application.ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "ok"}})
	if err != nil || !ack.Accepted {
		t.Errorf("report: %v %+v", err, ack)
	}
	// The token is spent.
	_, csr, _ := pki.NewNodeKeyAndRequest("web-1")
	anonymous, _ := pki.EnrollTLSConfig([]byte(token.CAPEM))
	var remote RemoteError
	if _, err := NewTLSClient(f.url, anonymous).Enroll(ctx, nodeproto.EnrollRequest{TokenID: token.ID, Secret: token.Secret, CSR: string(csr)}); !errors.As(err, &remote) || remote.Kind != KindState || !strings.Contains(err.Error(), "used at") {
		t.Errorf("reused token: %v", err)
	}
	// A token that expired.
	short, err := f.operator.CreateEnrollmentToken(ctx, EnrollTokenRequest{NodeID: "db-1", TTL: "1ms"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	expired, _ := nodeproto.DecodeEnrollToken(short.Token)
	if _, err := NewTLSClient(f.url, anonymous).Enroll(ctx, nodeproto.EnrollRequest{TokenID: expired.ID, Secret: expired.Secret, CSR: string(csr)}); !errors.As(err, &remote) || remote.Kind != KindState || !strings.Contains(err.Error(), "expired") {
		t.Errorf("expired token: %v", err)
	}
	// A wrong secret, an unknown token, a bad TTL.
	fresh := f.token(t, "db-2", nil)
	if _, err := NewTLSClient(f.url, anonymous).Enroll(ctx, nodeproto.EnrollRequest{TokenID: fresh.ID, Secret: "nope", CSR: string(csr)}); !domain.IsForbidden(err) {
		t.Errorf("wrong secret: %v", err)
	}
	if _, err := NewTLSClient(f.url, anonymous).Enroll(ctx, nodeproto.EnrollRequest{TokenID: "enr-404", Secret: "nope", CSR: string(csr)}); !domain.IsForbidden(err) {
		t.Errorf("unknown token: %v", err)
	}
	if _, err := f.operator.CreateEnrollmentToken(ctx, EnrollTokenRequest{TTL: "soon"}); err == nil {
		t.Error("a bad TTL was accepted")
	}
}

func TestTLSRefusals(t *testing.T) {
	f := serveFleet(t)
	ctx := context.Background()
	token := f.token(t, "web-1", nil)
	creds, node := f.enroll(t, token)
	if _, err := node.Register(ctx, application.NodeRegistration{ID: "web-1", HostName: "h"}); err != nil {
		t.Fatal(err)
	}
	// A node that trusts another CA cannot even connect.
	foreign, _ := pki.EnrollTLSConfig(f.other.CertificatePEM())
	if _, err := NewTLSClient(f.url, foreign).Heartbeat(ctx, "web-1"); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Errorf("wrong CA: %v", err)
	}
	// A certificate another CA issued is refused in the handshake.
	otherKey, otherCSR, _ := pki.NewNodeKeyAndRequest("web-1")
	otherIssued, _ := f.other.IssueNode(otherCSR, "web-1", 0)
	otherCreds, _ := pki.ParseCredentials(otherKey, otherIssued.CertificatePEM, f.ca.CertificatePEM())
	otherConfig, _ := otherCreds.ClientTLSConfig()
	if _, err := NewTLSClient(f.url, otherConfig).Heartbeat(ctx, "web-1"); err == nil {
		t.Error("a certificate of another fleet was accepted")
	}
	// No certificate: health and enrollment answer, nothing else does,
	// and the operator's routes are not there at all.
	anonymous, _ := pki.EnrollTLSConfig(f.ca.CertificatePEM())
	plain := NewTLSClient(f.url, anonymous)
	var health map[string]string
	if err := plain.call(ctx, http.MethodGet, "/healthz", nil, &health); err != nil || health["status"] != "ok" {
		t.Errorf("healthz: %v", err)
	}
	if _, err := plain.Heartbeat(ctx, "web-1"); !domain.IsForbidden(err) || !strings.Contains(err.Error(), "certificate is required") {
		t.Errorf("anonymous heartbeat: %v", err)
	}
	if _, err := plain.Register(ctx, application.NodeRegistration{ID: "web-9", HostName: "h"}); !domain.IsForbidden(err) {
		t.Errorf("anonymous register: %v", err)
	}
	if _, err := plain.ListBots(ctx); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("operator route over TLS: %v", err)
	}
	if _, err := node.ListBots(ctx); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("operator route with a node certificate: %v", err)
	}
	// The certificate names web-1: it cannot speak for web-2.
	if _, err := node.Heartbeat(ctx, "web-2"); !domain.IsForbidden(err) || !strings.Contains(err.Error(), "the request names web-2") {
		t.Errorf("mismatched path: %v", err)
	}
	if _, err := node.Register(ctx, application.NodeRegistration{ID: "web-2", HostName: "h"}); !domain.IsForbidden(err) || !strings.Contains(err.Error(), "the registration names web-2") {
		t.Errorf("mismatched body: %v", err)
	}
	if _, _, err := node.NextAssignment(ctx, "web-2", 0); !domain.IsForbidden(err) {
		t.Errorf("mismatched poll: %v", err)
	}
	if _, err := node.RenewLease(ctx, "web-2", "exec-1"); !domain.IsForbidden(err) {
		t.Errorf("mismatched lease: %v", err)
	}
	if _, err := node.Report(ctx, "web-2", "exec-1", application.ExecutionReport{Outcome: domain.ExecutionCompleted}); !domain.IsForbidden(err) {
		t.Errorf("mismatched report: %v", err)
	}
	// Revocation: the serial is refused at the next request, the node is
	// Revoked, and what it ran is failed.
	if _, err := f.operator.RegisterBot(ctx, definition("bot-a", "observe")); err != nil {
		t.Fatal(err)
	}
	_, _ = f.operator.EnableBot(ctx, "bot-a")
	submitted, _ := f.operator.SubmitTask(ctx, SubmitRequest{BotID: "bot-a", Objective: "list", IdempotencyKey: "k-1"})
	if _, found, err := node.NextAssignment(ctx, "web-1", 5*time.Second); err != nil || !found {
		t.Fatalf("assignment: %v %v", err, found)
	}
	revoked, err := f.operator.RevokeNode(ctx, "web-1")
	if err != nil || revoked.Node.Status != "Revoked" || len(revoked.FailedTasks) != 1 || revoked.FailedTasks[0] != submitted.Task.ID {
		t.Fatalf("revoke: %v %+v", err, revoked)
	}
	if task, _ := f.operator.GetTask(ctx, submitted.Task.ID); task.Status != "Failed" || !strings.Contains(task.FailureReason, "revoked: node web-1") {
		t.Errorf("task = %+v", task)
	}
	if _, err := node.Heartbeat(ctx, "web-1"); !domain.IsForbidden(err) || !strings.Contains(err.Error(), "revoked") {
		t.Errorf("revoked heartbeat: %v", err)
	}
	if _, err := node.Register(ctx, application.NodeRegistration{ID: "web-1", HostName: "h"}); !domain.IsForbidden(err) {
		t.Errorf("revoked register: %v", err)
	}
	// A new connection with the revoked certificate is refused too.
	freshConfig, _ := creds.ClientTLSConfig()
	if _, err := NewTLSClient(f.url, freshConfig).Heartbeat(ctx, "web-1"); !domain.IsForbidden(err) {
		t.Errorf("revoked on a new connection: %v", err)
	}
	if _, err := f.operator.RevokeNode(ctx, "web-1"); err == nil {
		t.Error("revoked twice")
	}
	if _, err := f.operator.RevokeNode(ctx, "Bad Id"); err == nil {
		t.Error("bad id revoked")
	}
	// Readmitted with a fresh token: the new certificate works, the old
	// one stays refused.
	again := f.token(t, "web-1", nil)
	_, readmitted := f.enroll(t, again)
	if registered, err := readmitted.Register(ctx, application.NodeRegistration{HostName: "h"}); err != nil || registered.Status != "Online" {
		t.Errorf("readmitted: %v %+v", err, registered)
	}
	if _, err := node.Heartbeat(ctx, "web-1"); !domain.IsForbidden(err) {
		t.Errorf("old certificate after readmission: %v", err)
	}
	// Rotation: enrolling again supersedes the certificate in use.
	rotation := f.token(t, "web-1", nil)
	_, rotated := f.enroll(t, rotation)
	if _, err := rotated.Heartbeat(ctx, "web-1"); err != nil {
		t.Errorf("rotated: %v", err)
	}
	if _, err := readmitted.Heartbeat(ctx, "web-1"); !domain.IsForbidden(err) || !strings.Contains(err.Error(), "not the current one") {
		t.Errorf("superseded certificate: %v", err)
	}
	// A certificate whose common name is not a node id.
	badKey, badCSR, _ := pki.NewNodeKeyAndRequest("Bad Id")
	badIssued, err := f.ca.IssueNode(badCSR, "Bad Id", 0)
	if err != nil {
		t.Fatal(err)
	}
	badCreds, _ := pki.ParseCredentials(badKey, badIssued.CertificatePEM, f.ca.CertificatePEM())
	badConfig, _ := badCreds.ClientTLSConfig()
	if _, err := NewTLSClient(f.url, badConfig).Heartbeat(ctx, "web-1"); !domain.IsForbidden(err) || !strings.Contains(err.Error(), "common name") {
		t.Errorf("bad common name: %v", err)
	}
	_ = tls.VersionTLS13
}

func TestDaemonRefusesTLSWithoutAConfiguration(t *testing.T) {
	if _, err := Listen("300.0.0.1:0"); err == nil {
		t.Error("a bad address listened")
	}
}
