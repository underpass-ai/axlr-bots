package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/pki"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/compose"
	"github.com/underpass-ai/axlr-bots/internal/fakeaxlr"
	"github.com/underpass-ai/axlr-bots/nodeproto"
)

// tlsControlPlane serves the node protocol over TLS and returns the
// operator's client, the https URL and the CA.
func tlsControlPlane(t *testing.T) (*unixapi.Client, string, pki.Authority) {
	t.Helper()
	dir := t.TempDir()
	state := filepath.Join(dir, "cp")
	deps, err := compose.Dependencies(state, "/nonexistent", "test")
	if err != nil {
		t.Fatal(err)
	}
	ca, err := pki.Generate(filepath.Join(state, pki.CADirName))
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "cp.sock")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := unixapi.NewServer(unixapi.NewLocal(deps))
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 2)
	go func() { served <- server.Serve(ctx, socket) }()
	go func() { served <- server.ServeTLS(ctx, listener, pki.ServerTLSConfig(ca, []string{"127.0.0.1"})) }()
	t.Cleanup(func() { cancel(); <-served; <-served })
	for !unixapi.Reachable(context.Background(), socket) {
		time.Sleep(10 * time.Millisecond)
	}
	return unixapi.NewClient(socket), "https://" + listener.Addr().String(), ca
}

func TestEnrollThenRunOverTLS(t *testing.T) {
	operator, url, ca := tlsControlPlane(t)
	minted, err := operator.CreateEnrollmentToken(context.Background(), unixapi.EnrollTokenRequest{NodeID: "web-2", Labels: map[string]string{"zone": "eu"}})
	if err != nil {
		t.Fatal(err)
	}
	nodeDir := filepath.Join(t.TempDir(), "node")
	var stdout, stderr bytes.Buffer
	// Running over https before enrolling says so.
	if code := run([]string{"--control", url, "--state", nodeDir}, &stdout, &stderr); code != exitError || !strings.Contains(stderr.String(), "enroll first") {
		t.Fatalf("not enrolled: %d %s", code, stderr.String())
	}
	stderr.Reset()
	if code := run([]string{"enroll", "--control", url, "--token", minted.Token, "--state", nodeDir, "--host-name", "web-2.example"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("enroll: %d %s", code, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result["nodeId"] != "web-2" || result["serial"] == "" || result["state"] != nodeDir || result["labels"].(map[string]any)["zone"] != "eu" {
		t.Fatalf("result = %s %v", stdout.String(), err)
	}
	if info, err := os.Stat(filepath.Join(nodeDir, "pki", "node.key")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("node.key: %v", err)
	}
	if record, err := loadRecord(nodeDir); err != nil || record.Control != url || record.Labels["zone"] != "eu" || record.NodeID != "web-2" {
		t.Errorf("record = %+v %v", record, err)
	}
	// The token is spent.
	stderr.Reset()
	if code := run([]string{"enroll", "--control", url, "--token", minted.Token, "--state", filepath.Join(t.TempDir(), "again")}, &stdout, &stderr); code != exitError || !strings.Contains(stderr.String(), "used at") {
		t.Errorf("reused token: %d %s", code, stderr.String())
	}
	// A --node-id that disagrees with the certificate is refused.
	stderr.Reset()
	if code := run([]string{"--control", url, "--state", nodeDir, "--node-id", "web-9"}, &stdout, &stderr); code != exitError || !strings.Contains(stderr.String(), "disagrees with the certificate") {
		t.Errorf("mismatched node id: %d %s", code, stderr.String())
	}
	// The agent runs with the certificate: the control plane sees the
	// node Online with the serial and the enrollment's labels plus the
	// flag's, and the node runs a task.
	bin, _, _ := fakeaxlr.Write(t.TempDir(), fakeaxlr.Options{Scenario: "completed"})
	stdout.Reset()
	stderr.Reset()
	done := make(chan int, 1)
	go func() {
		done <- run([]string{"--control", url, "--state", nodeDir, "--labels", "rack=r1", "--axlr-bin", bin, "--heartbeat", "50ms", "--poll", "100ms"}, &stdout, &stderr)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		node, err := operator.GetNode(context.Background(), "web-2")
		if err == nil && node.Status == "Online" && node.CertificateSerial == result["serial"] && node.Labels["zone"] == "eu" && node.Labels["rack"] == "r1" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("node not online: %+v %v\n%s", node, err, stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := operator.RegisterBot(context.Background(), application.BotDefinition{Tenant: "t", Name: "bot", Owner: "o", Mission: "m"}); err != nil {
		t.Fatal(err)
	}
	if _, err := operator.EnableBot(context.Background(), "bot"); err != nil {
		t.Fatal(err)
	}
	submitted, _ := operator.SubmitTask(context.Background(), unixapi.SubmitRequest{BotID: "bot", Objective: "o", IdempotencyKey: "k"})
	deadline = time.Now().Add(5 * time.Second)
	for {
		task, _ := operator.GetTask(context.Background(), submitted.Task.ID)
		if task.Status == "Completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task not run: %+v\n%s", task, stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	select {
	case code := <-done:
		if code != exitOK {
			t.Errorf("exit %d: %s", code, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("node did not stop")
	}
	// --ca-file in place of the CA the token carries.
	caFile := filepath.Join(t.TempDir(), "ca.crt")
	_ = os.WriteFile(caFile, ca.CertificatePEM(), 0o644)
	other, _ := operator.CreateEnrollmentToken(context.Background(), unixapi.EnrollTokenRequest{NodeID: "db-1"})
	stderr.Reset()
	if code := run([]string{"enroll", "--control", url, "--token", other.Token, "--ca-file", caFile, "--state", filepath.Join(t.TempDir(), "db")}, &stdout, &stderr); code != exitOK {
		t.Errorf("enroll with --ca-file: %d %s", code, stderr.String())
	}
	// A wrong CA file: the server is not trusted.
	wrong, _ := pki.Generate(filepath.Join(t.TempDir(), "wrong"))
	_ = os.WriteFile(caFile, wrong.CertificatePEM(), 0o644)
	third, _ := operator.CreateEnrollmentToken(context.Background(), unixapi.EnrollTokenRequest{NodeID: "db-2"})
	stderr.Reset()
	if code := run([]string{"enroll", "--control", url, "--token", third.Token, "--ca-file", caFile, "--state", filepath.Join(t.TempDir(), "db2")}, &stdout, &stderr); code != exitError || !strings.Contains(stderr.String(), "certificate") {
		t.Errorf("enroll with the wrong CA: %d %s", code, stderr.String())
	}
}

func TestEnrollUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	for _, tc := range []struct {
		args []string
		code int
		text string
	}{
		{[]string{"enroll", "--bogus"}, exitUsage, "flag provided"},
		{[]string{"enroll", "extra"}, exitUsage, "flags only"},
		{[]string{"enroll", "--control", "unix:///x.sock", "--token", "t"}, exitUsage, "https://"},
		{[]string{"enroll", "--control", "https://cp:7878"}, exitUsage, "--token"},
		{[]string{"enroll", "--control", "https://cp:7878", "--token", "nope"}, exitUsage, "enrollment token"},
	} {
		stderr.Reset()
		if code := run(tc.args, &stdout, &stderr); code != tc.code || !strings.Contains(stderr.String(), tc.text) {
			t.Errorf("%v: %d %q", tc.args, code, stderr.String())
		}
	}
	if code := run([]string{"enroll", "--help"}, &stdout, &stderr); code != exitOK {
		t.Errorf("enroll --help = %d", code)
	}
	token := (nodeproto.EnrollToken{ID: "enr-1", Secret: "s3cret-s3cret-s3cret", NodeID: "web-2", CAPEM: "junk"}).Encode()
	stderr.Reset()
	if code := run([]string{"enroll", "--control", "https://127.0.0.1:1", "--token", token, "--ca-file", "/nonexistent/ca.crt"}, &stdout, &stderr); code != exitError {
		t.Errorf("missing ca file: %d %s", code, stderr.String())
	}
	stderr.Reset()
	if code := run([]string{"enroll", "--control", "https://127.0.0.1:1", "--token", token, "--state", t.TempDir()}, &stdout, &stderr); code != exitError || !strings.Contains(stderr.String(), "no certificate") {
		t.Errorf("junk CA in the token: %d %s", code, stderr.String())
	}
	ca, _ := pki.Generate(filepath.Join(t.TempDir(), "ca"))
	token = (nodeproto.EnrollToken{ID: "enr-1", Secret: "s3cret-s3cret-s3cret", NodeID: "web-2", CAPEM: string(ca.CertificatePEM())}).Encode()
	stderr.Reset()
	if code := run([]string{"enroll", "--control", "https://127.0.0.1:1", "--token", token, "--state", t.TempDir()}, &stdout, &stderr); code != exitError || !strings.Contains(stderr.String(), "connection refused") {
		t.Errorf("no control plane: %d %s", code, stderr.String())
	}
	bad := (nodeproto.EnrollToken{ID: "enr-1", Secret: "s3cret-s3cret-s3cret", NodeID: "Bad Id", CAPEM: string(ca.CertificatePEM())}).Encode()
	stderr.Reset()
	if code := run([]string{"enroll", "--control", "https://127.0.0.1:1", "--token", bad, "--state", t.TempDir()}, &stdout, &stderr); code != exitError || !strings.Contains(stderr.String(), "node id") {
		t.Errorf("bad node id in the token: %d %s", code, stderr.String())
	}
	// A broken record beside valid credentials.
	nodeDir := t.TempDir()
	key, csr, _ := pki.NewNodeKeyAndRequest("web-2")
	issued, _ := ca.IssueNode(csr, "web-2", 0)
	_ = pki.SaveCredentials(nodeDir, key, issued.CertificatePEM, ca.CertificatePEM())
	_ = os.WriteFile(recordPath(nodeDir), []byte("{broken"), 0o644)
	stderr.Reset()
	if code := run([]string{"--control", "https://127.0.0.1:1", "--state", nodeDir}, &stdout, &stderr); code != exitError {
		t.Errorf("broken record: %d %s", code, stderr.String())
	}
}
