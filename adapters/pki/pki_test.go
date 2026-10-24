package pki

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
)

func TestGenerateLoadAndRefuseToOverwrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ca")
	ca, err := Generate(dir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "ca.key"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("ca.key mode = %v %v", info.Mode(), err)
	}
	if _, err := Generate(dir); err == nil || !strings.Contains(err.Error(), "already initialised") {
		t.Errorf("overwrite: %v", err)
	}
	loaded, err := Load(dir)
	if err != nil || loaded.Fingerprint() != ca.Fingerprint() || len(loaded.Fingerprint()) != 64 || !loaded.Certificate().IsCA {
		t.Errorf("load: %v %s", err, loaded.Fingerprint())
	}
	if string(loaded.CertificatePEM()) != string(ca.CertificatePEM()) || !strings.HasPrefix(string(ca.CertificatePEM()), "-----BEGIN CERTIFICATE-----") {
		t.Error("certificate PEM differs")
	}
	if _, err := Load(t.TempDir()); err == nil || !strings.Contains(err.Error(), "ca init") {
		t.Errorf("missing CA: %v", err)
	}
	if _, err := Generate(filepath.Join(filepath.Join(dir, "ca.key"), "sub")); err == nil {
		t.Error("a file as a directory was accepted")
	}
	_ = os.Remove(filepath.Join(dir, "ca.crt"))
	if _, err := Load(dir); err == nil {
		t.Error("missing ca.crt accepted")
	}
}

func TestParseRefusesBadMaterial(t *testing.T) {
	ca, _ := Generate(filepath.Join(t.TempDir(), "ca"))
	keyPEM, _ := os.ReadFile(filepath.Join(filepath.Dir(filepath.Join(t.TempDir(), "x")), "nope"))
	_ = keyPEM
	nodeKey, csr, err := NewNodeKeyAndRequest("web-1")
	if err != nil {
		t.Fatal(err)
	}
	issued, err := ca.IssueNode(csr, "web-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][2][]byte{
		"not pem cert":  {[]byte("junk"), nil},
		"not pem key":   {ca.CertificatePEM(), []byte("junk")},
		"not pkcs8 key": {ca.CertificatePEM(), csr},
		"not a CA":      {issued.CertificatePEM, nodeKey},
	}
	for name, material := range cases {
		if _, err := Parse(material[0], material[1]); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestIssueNodeChecksTheRequest(t *testing.T) {
	ca, _ := Generate(filepath.Join(t.TempDir(), "ca"))
	_, csr, _ := NewNodeKeyAndRequest("web-1")
	issued, err := ca.IssueNode(csr, "web-1", time.Hour)
	if err != nil || issued.Serial == "" || time.Until(issued.NotAfter) > 2*time.Hour {
		t.Fatalf("issue: %v %+v", err, issued)
	}
	leaf, err := parseCertificate(issued.CertificatePEM)
	if err != nil || leaf.Subject.CommonName != "web-1" || SerialOf(leaf) != issued.Serial || leaf.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Errorf("leaf = %+v %v", leaf.Subject, err)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: ca.Pool(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Errorf("verify: %v", err)
	}
	if _, err := ca.IssueNode(csr, "db-1", 0); err == nil || !strings.Contains(err.Error(), `the token is for "db-1"`) {
		t.Errorf("wrong name: %v", err)
	}
	if _, err := ca.IssueNode([]byte("junk"), "web-1", 0); err == nil {
		t.Error("junk CSR accepted")
	}
	if _, err := ca.IssueNode(ca.CertificatePEM(), "web-1", 0); err == nil {
		t.Error("a certificate as a CSR accepted")
	}
	// A request signed by another key: the public key of one, the
	// signature of the other.
	_, otherCSR, _ := NewNodeKeyAndRequest("web-1")
	end := strings.Index(string(csr), "-----END")
	swapped := string(csr)[:end-40] + string(otherCSR)[end-40:]
	if _, err := ca.IssueNode([]byte(swapped), "web-1", 0); err == nil {
		t.Error("a request with a bad signature accepted")
	}
}

func TestCredentialsRoundTrip(t *testing.T) {
	ca, _ := Generate(filepath.Join(t.TempDir(), "ca"))
	key, csr, _ := NewNodeKeyAndRequest("web-1")
	issued, _ := ca.IssueNode(csr, "web-1", 0)
	dir := t.TempDir()
	if _, err := LoadCredentials(dir); err == nil || !strings.Contains(err.Error(), "enroll first") {
		t.Errorf("not enrolled: %v", err)
	}
	if err := SaveCredentials(dir, key, issued.CertificatePEM, ca.CertificatePEM()); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(dir, "pki", "node.key"))
	if info.Mode().Perm() != 0o600 {
		t.Errorf("node.key mode = %v", info.Mode())
	}
	creds, err := LoadCredentials(dir)
	if err != nil || creds.NodeID != "web-1" || creds.Serial != issued.Serial || !creds.NotAfter.Equal(issued.NotAfter) {
		t.Fatalf("load: %v %+v", err, creds)
	}
	if _, err := creds.ClientTLSConfig(); err != nil {
		t.Error(err)
	}
	if _, err := ParseCredentials(key, issued.CertificatePEM, []byte("junk")); err == nil {
		t.Error("junk CA accepted")
	}
	if _, err := ParseCredentials([]byte("junk"), issued.CertificatePEM, ca.CertificatePEM()); err == nil {
		t.Error("junk key accepted")
	}
	if _, err := EnrollTLSConfig([]byte("junk")); err == nil {
		t.Error("junk bundle accepted")
	}
	if _, err := (Credentials{CAPEM: []byte("junk")}).ClientTLSConfig(); err == nil {
		t.Error("junk CA in credentials accepted")
	}
	_ = os.Remove(filepath.Join(dir, "pki", "ca.crt"))
	if _, err := LoadCredentials(dir); err == nil {
		t.Error("missing ca.crt accepted")
	}
	_ = os.Remove(filepath.Join(dir, "pki", "node.key"))
	if _, err := LoadCredentials(dir); err == nil {
		t.Error("missing node.key accepted")
	}
	if err := SaveCredentials(filepath.Join(dir, "pki", "node.crt"), key, issued.CertificatePEM, ca.CertificatePEM()); err == nil {
		t.Error("a file as the state directory accepted")
	}
}

// TestHandshakes runs the server configuration against a node's
// credentials, a node from another fleet and a node with no certificate.
func TestHandshakes(t *testing.T) {
	ca, _ := Generate(filepath.Join(t.TempDir(), "ca"))
	other, _ := Generate(filepath.Join(t.TempDir(), "other"))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			_, _ = io.WriteString(w, "anonymous")
			return
		}
		_, _ = io.WriteString(w, r.TLS.PeerCertificates[0].Subject.CommonName+" "+SerialOf(r.TLS.PeerCertificates[0]))
	}), TLSConfig: ServerTLSConfig(ca, []string{"127.0.0.1", "control.example"}), ErrorLog: log.New(io.Discard, "", 0)}
	go func() { _ = server.ServeTLS(listener, "", "") }()
	defer server.Close()
	url := "https://" + listener.Addr().String()
	get := func(config *tls.Config) (string, error) {
		client := &http.Client{Transport: &http.Transport{TLSClientConfig: config}}
		response, err := client.Get(url)
		if err != nil {
			return "", err
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		return string(body), nil
	}
	key, csr, _ := NewNodeKeyAndRequest("web-1")
	issued, _ := ca.IssueNode(csr, "web-1", 0)
	creds, _ := ParseCredentials(key, issued.CertificatePEM, ca.CertificatePEM())
	config, _ := creds.ClientTLSConfig()
	if body, err := get(config); err != nil || body != "web-1 "+issued.Serial {
		t.Errorf("enrolled node: %q %v", body, err)
	}
	if config.MinVersion != tls.VersionTLS13 {
		t.Error("TLS 1.3 not required")
	}
	// A node that trusts another CA refuses the server.
	foreign, _ := EnrollTLSConfig(other.CertificatePEM())
	if _, err := get(foreign); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Errorf("wrong CA accepted: %v", err)
	}
	// A certificate from another CA is refused in the handshake.
	otherKey, otherCSR, _ := NewNodeKeyAndRequest("web-1")
	otherIssued, _ := other.IssueNode(otherCSR, "web-1", 0)
	otherCreds, _ := ParseCredentials(otherKey, otherIssued.CertificatePEM, ca.CertificatePEM())
	otherConfig, _ := otherCreds.ClientTLSConfig()
	if _, err := get(otherConfig); err == nil {
		t.Error("a certificate of another fleet was accepted")
	}
	// No certificate: the handshake passes (enrollment needs it); the
	// routes decide.
	anonymous, _ := EnrollTLSConfig(ca.CertificatePEM())
	if body, err := get(anonymous); err != nil || body != "anonymous" {
		t.Errorf("anonymous: %q %v", body, err)
	}
	// The server name must match: dialling by another name fails.
	byName := anonymous.Clone()
	byName.ServerName = "elsewhere.example"
	if _, err := get(byName); err == nil {
		t.Error("another server name accepted")
	}
	byName.ServerName = "control.example"
	if _, err := get(byName); err != nil {
		t.Errorf("the DNS SAN was not honoured: %v", err)
	}
}

func TestServerCertificateIsReissuedNearExpiry(t *testing.T) {
	ca, _ := Generate(filepath.Join(t.TempDir(), "ca"))
	issuer := &serverCertificate{authority: ca, hosts: []string{"localhost"}}
	first, err := issuer.get(nil)
	if err != nil || first.Leaf.DNSNames[0] != "localhost" {
		t.Fatalf("first: %v", err)
	}
	if again, _ := issuer.get(nil); again != first {
		t.Error("a fresh certificate was replaced")
	}
	expiring, _ := ca.IssueServer([]string{"localhost"}, time.Hour)
	issuer.current = &expiring
	if renewed, _ := issuer.get(nil); renewed == &expiring {
		t.Error("an expiring certificate was kept")
	}
	// A listener the config serves on, end to end.
	listener, _ := net.Listen("tcp", "127.0.0.1:0")
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		_ = tls.Server(conn, ServerTLSConfig(ca, []string{"127.0.0.1"})).HandshakeContext(context.Background())
		_ = conn.Close()
	}()
	anonymous, _ := EnrollTLSConfig(ca.CertificatePEM())
	conn, err := tls.Dial("tcp", listener.Addr().String(), anonymous)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
}

func TestCertifierOverTheStateDirectory(t *testing.T) {
	var _ application.Certifier = Certifier{}
	state := t.TempDir()
	certifier := NewCertifier(state)
	if certifier.Dir() != filepath.Join(state, "ca") {
		t.Error(certifier.Dir())
	}
	ctx := context.Background()
	if _, err := certifier.CACertificate(ctx); err == nil || !strings.Contains(err.Error(), "ca init") {
		t.Errorf("without a CA: %v", err)
	}
	_, csr, _ := NewNodeKeyAndRequest("web-1")
	if _, err := certifier.IssueNodeCertificate(ctx, "web-1", csr); err == nil {
		t.Error("issued without a CA")
	}
	ca, err := Generate(certifier.Dir())
	if err != nil {
		t.Fatal(err)
	}
	pemCA, err := certifier.CACertificate(ctx)
	if err != nil || string(pemCA) != string(ca.CertificatePEM()) {
		t.Errorf("CA: %v", err)
	}
	issued, err := certifier.IssueNodeCertificate(ctx, "web-1", csr)
	if err != nil || issued.Serial == "" || time.Until(issued.NotAfter) < 364*24*time.Hour {
		t.Errorf("issue: %v %+v", err, issued)
	}
	if _, err := certifier.IssueNodeCertificate(ctx, "db-1", csr); err == nil {
		t.Error("issued for another node")
	}
}
