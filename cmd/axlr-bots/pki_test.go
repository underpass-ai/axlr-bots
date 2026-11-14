package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/pki"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/nodeproto"
)

func TestCAInitAndServeOverTLS(t *testing.T) {
	state := t.TempDir()
	socket := filepath.Join(t.TempDir(), "cp.sock")
	c := cli{t: t, state: state, axlr: "/nonexistent/axlr-tui", socket: ""}
	// serve --listen needs the CA.
	var out, errText bytes.Buffer
	if code := run([]string{"--state", state, "--socket", socket, "serve", "--listen", "127.0.0.1:0"}, &out, &errText); code != exitError || !strings.Contains(errText.String(), "ca init") {
		t.Fatalf("serve without a CA: %d %s", code, errText.String())
	}
	if code, _, errText := c.run("ca", "show"); code != exitError || !strings.Contains(errText, "ca init") {
		t.Errorf("ca show without a CA: %d %s", code, errText)
	}
	code, report := c.json("ca", "init")
	if code != exitOK || report["fingerprint"] == "" || !strings.Contains(report["certificate"].(string), "BEGIN CERTIFICATE") || report["dir"] != filepath.Join(state, "ca") {
		t.Fatalf("ca init: %d %v", code, report)
	}
	if code, _, errText := c.run("ca", "init"); code != exitError || !strings.Contains(errText, "already initialised") {
		t.Errorf("ca init twice: %d %s", code, errText)
	}
	if code, shown := c.json("ca", "show"); code != exitOK || shown["fingerprint"] != report["fingerprint"] {
		t.Errorf("ca show: %d %v", code, shown)
	}
	for _, args := range [][]string{{"ca", "init", "extra"}, {"ca", "show", "extra"}, {"node", "enroll-token", "extra"}, {"node", "enroll-token", "--labels", "novalue"}, {"node", "revoke"}, {"node", "enroll-token", "--bogus"}} {
		if code, _, _ := c.run(args...); code != exitUsage {
			t.Errorf("%v: %d", args, code)
		}
	}
	// A token without a daemon works on the state directory; the daemon
	// then serves the enrollment over TLS.
	code, minted := c.json("node", "enroll-token", "--node-id", "web-2", "--labels", "zone=eu", "--ttl", "30m")
	if code != exitOK || minted["nodeId"] != "web-2" || minted["labels"].(map[string]any)["zone"] != "eu" {
		t.Fatalf("enroll-token: %d %v", code, minted)
	}
	port := freePort(t)
	var serveOut, serveErr syncBuffer
	done := make(chan int, 1)
	go func() {
		done <- run([]string{"--state", state, "--axlr-bin", c.axlr, "--socket", socket, "serve", "--listen", "127.0.0.1:" + port, "--server-names", "cp.example", "--tick", "20ms"}, &serveOut, &serveErr)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !daemonAnswers(t.Context(), state, socket) {
		if time.Now().After(deadline) {
			t.Fatalf("daemon not reachable: %s", serveErr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	token, err := nodeproto.DecodeEnrollToken(minted["token"].(string))
	if err != nil {
		t.Fatal(err)
	}
	url := "https://127.0.0.1:" + port
	key, csr, _ := pki.NewNodeKeyAndRequest("web-2")
	config, _ := pki.EnrollTLSConfig([]byte(token.CAPEM))
	var response nodeproto.EnrollResponse
	deadline = time.Now().Add(5 * time.Second)
	for {
		response, err = unixapi.NewTLSClient(url, config).Enroll(context.Background(), nodeproto.EnrollRequest{TokenID: token.ID, Secret: token.Secret, HostName: "web-2", CSR: string(csr)})
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("enroll: %v\n%s", err, serveErr.String())
	}
	creds, _ := pki.ParseCredentials(key, []byte(response.Certificate), []byte(response.CA))
	nodeConfig, _ := creds.ClientTLSConfig()
	node := unixapi.NewTLSClient(url, nodeConfig)
	if _, err := node.Register(context.Background(), application.NodeRegistration{HostName: "web-2"}); err != nil {
		t.Fatal(err)
	}
	c.socket = socket
	if code, got := c.json("node", "get", "web-2"); code != exitOK || got["status"] != "Online" || got["certificateSerial"] != response.Serial || got["labels"].(map[string]any)["zone"] != "eu" {
		t.Errorf("node get: %d %v", code, got)
	}
	code, revoked := c.json("node", "revoke", "web-2")
	if code != exitOK || revoked["node"].(map[string]any)["status"] != "Revoked" {
		t.Errorf("revoke: %d %v", code, revoked)
	}
	if _, err := node.Heartbeat(context.Background(), "web-2"); !domain.IsForbidden(err) {
		t.Errorf("revoked heartbeat: %v", err)
	}
	if code, _, errText := c.run("node", "revoke", "web-2"); code != exitError || !strings.Contains(errText, "Revoked") {
		t.Errorf("revoke twice: %d %s", code, errText)
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	select {
	case code := <-done:
		if code != exitOK {
			t.Errorf("serve exit %d: %s", code, serveErr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop")
	}
	if !strings.Contains(serveErr.String(), "and https://127.0.0.1:"+port) {
		t.Errorf("serve log = %s", serveErr.String())
	}
}

func TestCAIssueClientWritesAClientCertificateOfTheFleet(t *testing.T) {
	state := t.TempDir()
	c := cli{t: t, state: state, axlr: "/nonexistent/axlr-tui"}
	out := filepath.Join(t.TempDir(), "certs")
	if code, _, errText := c.run("ca", "issue-client", "--name", "control-plane", "--out", out); code != exitError || !strings.Contains(errText, "ca init") {
		t.Errorf("issue-client without a CA: %d %s", code, errText)
	}
	if code, _ := c.json("ca", "init"); code != exitOK {
		t.Fatalf("ca init: %d", code)
	}
	code, report := c.json("ca", "issue-client", "--name", "control-plane", "--out", out)
	if code != exitOK || report["subject"] != "CN=control-plane,O=axlr-bots clients" || report["dir"] != out || report["ca"] == "" {
		t.Fatalf("issue-client: %d %v", code, report)
	}
	if info, err := os.Stat(filepath.Join(out, "client.key")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("client.key: %v %v", err, info)
	}
	leaf := clientCertificate(t, out)
	if leaf.Subject.CommonName != "control-plane" || len(leaf.Subject.Organization) != 1 || leaf.Subject.Organization[0] != pki.ClientOrganization {
		t.Errorf("subject %v", leaf.Subject)
	}
	if !slices.Contains(leaf.ExtKeyUsage, x509.ExtKeyUsageClientAuth) {
		t.Errorf("usage %v", leaf.ExtKeyUsage)
	}
	if lifetime := time.Until(leaf.NotAfter); lifetime < 364*24*time.Hour || lifetime > 366*24*time.Hour {
		t.Errorf("default lifetime %s", lifetime)
	}
	// The key is the certificate's: the pair loads together.
	if _, err := tls.LoadX509KeyPair(filepath.Join(out, "client.crt"), filepath.Join(out, "client.key")); err != nil {
		t.Errorf("key pair: %v", err)
	}
	// A second run keeps the key it finds.
	before, _ := os.ReadFile(filepath.Join(out, "client.key"))
	if code, _, errText := c.run("ca", "issue-client", "--name", "control-plane", "--out", out); code != exitError || !strings.Contains(errText, "exists") {
		t.Errorf("issue-client twice: %d %s", code, errText)
	}
	if after, _ := os.ReadFile(filepath.Join(out, "client.key")); !bytes.Equal(before, after) {
		t.Error("the second run replaced the key")
	}
	short := filepath.Join(t.TempDir(), "short")
	if code, _ := c.json("ca", "issue-client", "--name", "nats", "--out", short, "--lifetime", "1h"); code != exitOK {
		t.Fatalf("issue-client --lifetime: %d", code)
	}
	if lifetime := time.Until(clientCertificate(t, short).NotAfter); lifetime < 55*time.Minute || lifetime > time.Hour+time.Minute {
		t.Errorf("--lifetime 1h gave %s", lifetime)
	}
	other := filepath.Join(t.TempDir(), "other")
	for _, args := range [][]string{
		{"ca", "issue-client", "--out", other},
		{"ca", "issue-client", "--name", "control-plane"},
		{"ca", "issue-client", "--name", "control-plane", "--out", other, "extra"},
		{"ca", "issue-client", "--bogus"},
	} {
		if code, _, _ := c.run(args...); code != exitUsage {
			t.Errorf("%v: %d", args, code)
		}
	}
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Errorf("a refused run wrote %s: %v", other, err)
	}
}

// clientCertificate reads the client.crt under dir and verifies it
// against the ca.crt beside it, for client authentication.
func clientCertificate(t *testing.T, dir string) *x509.Certificate {
	t.Helper()
	caPEM, err := os.ReadFile(filepath.Join(dir, "ca.crt"))
	if err != nil {
		t.Fatal(err)
	}
	certPEM, err := os.ReadFile(filepath.Join(dir, "client.crt"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatalf("client.crt is not PEM: %s", certPEM)
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatalf("ca.crt is not PEM: %s", caPEM)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Errorf("verify: %v", err)
	}
	return leaf
}

func TestServerNamesAndLabels(t *testing.T) {
	names := serverNames("127.0.0.1:7878", "cp.example, 10.0.0.9")
	joined := strings.Join(names, " ")
	for _, want := range []string{"127.0.0.1", "localhost", "::1", "cp.example", "10.0.0.9"} {
		if !strings.Contains(joined, want) {
			t.Errorf("names %v lack %s", names, want)
		}
	}
	if strings.Count(joined, "127.0.0.1") != 1 {
		t.Errorf("names repeat: %v", names)
	}
	all := serverNames("0.0.0.0:7878", "")
	if !strings.Contains(strings.Join(all, " "), "127.0.0.1") || strings.Contains(strings.Join(all, " "), "0.0.0.0") {
		t.Errorf("all interfaces: %v", all)
	}
	if bare := serverNames("cp.example", ""); bare[0] != "cp.example" {
		t.Errorf("bare host: %v", bare)
	}
	labels, err := parseLabels(" zone=eu , host = web-1 ,")
	if err != nil || labels["zone"] != "eu" || labels["host"] != "web-1" {
		t.Errorf("labels = %v %v", labels, err)
	}
	if _, err := parseLabels("=x"); err == nil {
		t.Error("an empty key parsed")
	}
}

// freePort finds a port the test may listen on.
func freePort(t *testing.T) string {
	t.Helper()
	listener, err := unixapi.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := splitHostPort(listener.Addr().String())
	_ = listener.Close()
	return port
}

func splitHostPort(address string) (string, string, bool) {
	i := strings.LastIndex(address, ":")
	if i < 0 {
		return address, "", false
	}
	return address[:i], address[i+1:], true
}
