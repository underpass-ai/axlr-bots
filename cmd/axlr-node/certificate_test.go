package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/pki"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/internal/fakeaxlr"
)

// The certificate under the state renews through the control plane: the
// new key and certificate replace the old on disk, and the client the
// agent holds presents the new one from then on.
func TestTheNodeCertificateRenewsIntoItsState(t *testing.T) {
	operator, url, _ := tlsControlPlane(t)
	ctx := context.Background()
	tokenFile, _ := mintToFile(t, operator, "web-3")
	state := filepath.Join(t.TempDir(), "node")
	if _, err := (firstStart{Control: url, TokenFile: tokenFile, StateDir: state}).ensure(ctx); err != nil {
		t.Fatal(err)
	}
	before, _ := pki.LoadCredentials(state)
	conn, err := transportFor(url, "", state)
	if err != nil || conn.certificate == nil || !conn.certificate.NotAfter().Equal(before.NotAfter) {
		t.Fatalf("connection: %v %+v", err, conn)
	}
	if _, err := conn.transport.Register(ctx, application.NodeRegistration{HostName: "web-3.example"}); err != nil {
		t.Fatal(err)
	}
	// A key that is not the request's is refused before anything is
	// written: the node keeps the certificate it has.
	stray, _, _ := conn.certificate.NewRequest("web-3")
	key, csr, err := conn.certificate.NewRequest("web-3")
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := conn.transport.RenewCertificate(ctx, "web-3", csr)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.certificate.Install(stray, renewed); err == nil {
		t.Error("a stray key was installed")
	}
	if kept, _ := pki.LoadCredentials(state); kept.Serial != before.Serial {
		t.Errorf("a refused install wrote %s", kept.Serial)
	}
	if err := conn.certificate.Install(key, renewed); err != nil {
		t.Fatal(err)
	}
	after, err := pki.LoadCredentials(state)
	if err != nil || after.Serial != renewed.Serial || after.Serial == before.Serial || !conn.certificate.NotAfter().Equal(after.NotAfter) {
		t.Errorf("after: %v %s (before %s)", err, after.Serial, before.Serial)
	}
	if info, err := os.Stat(filepath.Join(state, pki.CredentialsDirName, "node.key")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("node.key: %v", err)
	}
	if beat, err := conn.transport.Heartbeat(ctx, "web-3"); err != nil || beat.CertificateSerial != renewed.Serial {
		t.Errorf("heartbeat with the new certificate: %v %+v", err, beat)
	}
	// A certificate that is no PEM is refused.
	if err := conn.certificate.Install(key, application.RenewedCertificate{CertificatePEM: []byte("junk"), CAPEM: renewed.CAPEM}); err == nil {
		t.Error("junk installed")
	}
}

// An agent told to renew well before expiry renews at start: the
// control plane sees the new serial and the node logs one line.
func TestTheAgentRenewsAtStartWhenDue(t *testing.T) {
	operator, url, _ := tlsControlPlane(t)
	tokenFile, _ := mintToFile(t, operator, "web-4")
	state := filepath.Join(t.TempDir(), "node")
	if _, err := (firstStart{Control: url, TokenFile: tokenFile, StateDir: state}).ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, _ := pki.LoadCredentials(state)
	bin, _, _ := fakeaxlr.Write(t.TempDir(), fakeaxlr.Options{Scenario: "completed"})
	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- run([]string{"--control", url, "--state", state, "--axlr-bin", bin, "--heartbeat", "50ms", "--poll", "100ms", "--renew-before", "9000h"}, &stdout, &stderr)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		node, err := operator.GetNode(context.Background(), "web-4")
		if err == nil && node.Status == "Online" && node.CertificateSerial != before.Serial {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not renewed: %+v %v\n%s", node, err, stderr.String())
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
	after, _ := pki.LoadCredentials(state)
	if after.Serial == before.Serial || strings.Count(stderr.String(), "certificate renewed: serial "+after.Serial) != 1 {
		t.Errorf("after %s, log %s", after.Serial, stderr.String())
	}
	// check says when it expires and whether it is due.
	var out, errs bytes.Buffer
	if code := run([]string{"check", "--control", url, "--state", state}, &out, &errs); code != exitOK {
		t.Fatalf("check: %d %s", code, errs.String())
	}
	var answer map[string]string
	if err := json.Unmarshal(out.Bytes(), &answer); err != nil || answer["certificate"] != after.Serial || answer["notAfter"] != after.NotAfter.UTC().Format(time.RFC3339) || answer["renewalDue"] != "no" {
		t.Errorf("check = %s %v", out.String(), err)
	}
}

func TestCertificateStatusSaysWhenRenewalIsDue(t *testing.T) {
	ca, err := pki.Generate(filepath.Join(t.TempDir(), "ca"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for name, tc := range map[string]struct {
		lifetime time.Duration
		before   time.Duration
		due      string
	}{
		"far":     {lifetime: 100 * 24 * time.Hour, due: "no"},
		"soon":    {lifetime: 10 * 24 * time.Hour, due: "yes"},
		"asked":   {lifetime: 100 * 24 * time.Hour, before: 200 * 24 * time.Hour, due: "yes"},
		"expired": {lifetime: time.Nanosecond, due: "expired: enroll again with a fresh token"},
	} {
		state := t.TempDir()
		key, csr, _ := pki.NewNodeKeyAndRequest("web-5")
		issued, err := ca.IssueNode(csr, "web-5", tc.lifetime)
		if err != nil {
			t.Fatal(err)
		}
		if err := pki.SaveCredentials(state, key, issued.CertificatePEM, ca.CertificatePEM()); err != nil {
			t.Fatal(err)
		}
		at := now
		if name == "expired" {
			at = issued.NotAfter.Add(time.Second)
		}
		status := certificateStatus(state, at, tc.before)
		if status["renewalDue"] != tc.due || status["certificate"] != issued.Serial {
			t.Errorf("%s: %v", name, status)
		}
	}
	if status := certificateStatus(t.TempDir(), now, 0); status != nil {
		t.Errorf("not enrolled: %v", status)
	}
	t.Setenv("AXLR_NODE_RENEW_BEFORE", "48h")
	if envDuration("AXLR_NODE_RENEW_BEFORE", time.Hour) != 48*time.Hour || envDuration("AXLR_NODE_UNSET", time.Hour) != time.Hour {
		t.Error("envDuration")
	}
}
