package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/pki"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/internal/fakeaxlr"
)

// mintToFile mints a token for the node id and writes it to a file.
func mintToFile(t *testing.T, operator *unixapi.Client, nodeID string) (string, string) {
	t.Helper()
	minted, err := operator.CreateEnrollmentToken(context.Background(), unixapi.EnrollTokenRequest{NodeID: nodeID, Labels: map[string]string{"zone": "k8s"}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), nodeID)
	if err := os.WriteFile(path, []byte(minted.Token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, minted.ID
}

func TestFirstStartEnrollsKeepsAndRotates(t *testing.T) {
	operator, url, ca := tlsControlPlane(t)
	ctx := context.Background()
	state := filepath.Join(t.TempDir(), "node")
	missing := filepath.Join(t.TempDir(), "absent")

	// No certificate and no token: nothing to run with.
	if _, err := (firstStart{Control: url, TokenFile: missing, StateDir: state}).ensure(ctx); err == nil || !strings.Contains(err.Error(), "not enrolled") {
		t.Fatalf("no token: %v", err)
	}
	// A token that is not one.
	junk := filepath.Join(t.TempDir(), "junk")
	_ = os.WriteFile(junk, []byte("nope"), 0o600)
	if _, err := (firstStart{Control: url, TokenFile: junk, StateDir: state}).ensure(ctx); err == nil || !strings.Contains(err.Error(), "enrollment token") {
		t.Fatalf("junk token: %v", err)
	}
	// A token for another node is refused before it is sent: it still
	// enrolls the node it names afterwards.
	other, _ := mintToFile(t, operator, "pod-9")
	if _, err := (firstStart{Control: url, TokenFile: other, StateDir: state, NodeID: "pod-0"}).ensure(ctx); err == nil || !strings.Contains(err.Error(), "was not used") {
		t.Fatalf("token for another node: %v", err)
	}
	if done, err := (firstStart{Control: url, TokenFile: other, StateDir: filepath.Join(t.TempDir(), "pod-9")}).ensure(ctx); err != nil || !strings.Contains(done, "enrolled as node pod-9") {
		t.Fatalf("the refused token was spent: %q %v", done, err)
	}
	// A pinned CA that is not the fleet's: the control plane is not trusted.
	wrong, _ := pki.Generate(filepath.Join(t.TempDir(), "wrong"))
	wrongFile := filepath.Join(t.TempDir(), "wrong.crt")
	_ = os.WriteFile(wrongFile, wrong.CertificatePEM(), 0o644)
	first, firstID := mintToFile(t, operator, "pod-0")
	if _, err := (firstStart{Control: url, TokenFile: first, CAFile: wrongFile, StateDir: state, NodeID: "pod-0"}).ensure(ctx); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("wrong CA: %v", err)
	}
	if _, err := (firstStart{Control: url, TokenFile: first, CAFile: missing, StateDir: state, NodeID: "pod-0"}).ensure(ctx); err == nil {
		t.Fatal("a missing CA file was accepted")
	}
	// The first start enrolls with the fleet CA pinned.
	caFile := filepath.Join(t.TempDir(), "ca.crt")
	_ = os.WriteFile(caFile, ca.CertificatePEM(), 0o644)
	start := firstStart{Control: url, TokenFile: first, CAFile: caFile, StateDir: state, NodeID: "pod-0"}
	done, err := start.ensure(ctx)
	if err != nil || !strings.Contains(done, "enrolled as node pod-0 with token "+firstID) {
		t.Fatalf("first start: %q %v", done, err)
	}
	record, _ := loadRecord(state)
	if record.TokenID != firstID || record.NodeID != "pod-0" {
		t.Fatalf("record = %+v", record)
	}
	serial := record.Serial
	// Later starts keep the certificate: same token, no token, a broken one.
	for name, file := range map[string]string{"already used": first, "no token file": missing, "enrollment token": junk} {
		done, err := (firstStart{Control: url, TokenFile: file, StateDir: state, NodeID: "pod-0"}).ensure(ctx)
		if err != nil || !strings.Contains(done, "keeping the certificate for node pod-0") || !strings.Contains(done, name) {
			t.Errorf("%s: %q %v", name, done, err)
		}
	}
	// A token for another node, or one already spent, does not replace it.
	spent, _ := mintToFile(t, operator, "pod-0")
	if _, err := (firstStart{Control: url, TokenFile: spent, StateDir: filepath.Join(t.TempDir(), "elsewhere")}).ensure(ctx); err != nil {
		t.Fatal(err)
	}
	for name, file := range map[string]string{"another node": other, "spent": spent} {
		done, err := (firstStart{Control: url, TokenFile: file, StateDir: state, NodeID: "pod-0"}).ensure(ctx)
		if err != nil || !strings.Contains(done, "rotation:") {
			t.Errorf("failed rotation with a token for %s: %q %v", name, done, err)
		}
	}
	if record, _ := loadRecord(state); record.Serial != serial {
		t.Fatalf("a failed rotation changed the certificate: %+v", record)
	}
	// A fresh token for the same node rotates the certificate.
	fresh, freshID := mintToFile(t, operator, "pod-0")
	done, err = (firstStart{Control: url, TokenFile: fresh, StateDir: state, NodeID: "pod-0"}).ensure(ctx)
	if err != nil || !strings.Contains(done, "with token "+freshID) {
		t.Fatalf("rotation: %q %v", done, err)
	}
	if record, _ := loadRecord(state); record.Serial == serial || record.TokenID != freshID {
		t.Fatalf("rotation kept the old certificate: %+v", record)
	}
	// A node enrolled before the record named its token keeps it.
	record, _ = loadRecord(state)
	record.TokenID = ""
	_ = record.save(state)
	later, _ := mintToFile(t, operator, "pod-0")
	if done, err := (firstStart{Control: url, TokenFile: later, StateDir: state}).ensure(ctx); err != nil || !strings.Contains(done, "keeping") {
		t.Errorf("record without a token id: %q %v", done, err)
	}
	// A broken record is an error.
	_ = os.WriteFile(recordPath(state), []byte("{broken"), 0o644)
	if _, err := (firstStart{Control: url, TokenFile: later, StateDir: state}).ensure(ctx); err == nil {
		t.Error("a broken record was accepted")
	}
	// An unreadable token file (a directory) is an error before enrollment.
	if _, err := (firstStart{Control: url, TokenFile: t.TempDir(), StateDir: filepath.Join(t.TempDir(), "x")}).ensure(ctx); err == nil {
		t.Error("a directory was read as a token")
	}
}

func TestUntilEnrolledWaitsForTheToken(t *testing.T) {
	operator, url, _ := tlsControlPlane(t)
	state := filepath.Join(t.TempDir(), "node")
	tokenFile := filepath.Join(t.TempDir(), "pod-1")
	var lines []string
	logf := func(format string, args ...any) { lines = append(lines, format) }
	// The context ends while the token is missing.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if (firstStart{Control: url, TokenFile: tokenFile, StateDir: state}).untilEnrolled(ctx, 10*time.Millisecond, logf) {
		t.Fatal("enrolled without a token")
	}
	if len(lines) < 2 {
		t.Fatalf("retries logged: %v", lines)
	}
	// The token appears while the node waits (a Secret's update).
	minted, _ := operator.CreateEnrollmentToken(context.Background(), unixapi.EnrollTokenRequest{NodeID: "pod-1"})
	go func() {
		time.Sleep(30 * time.Millisecond)
		_ = os.WriteFile(tokenFile, []byte(minted.Token), 0o600)
	}()
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !(firstStart{Control: url, TokenFile: tokenFile, StateDir: state, NodeID: "pod-1"}).untilEnrolled(ctx, 10*time.Millisecond, logf) {
		t.Fatal("did not enroll once the token arrived")
	}
}

func TestRunWithTokenFile(t *testing.T) {
	operator, url, _ := tlsControlPlane(t)
	state := filepath.Join(t.TempDir(), "node")
	tokenFile, tokenID := mintToFile(t, operator, "demo-0")
	bin, _, _ := fakeaxlr.Write(t.TempDir(), fakeaxlr.Options{Scenario: "completed"})
	args := []string{"--control", url, "--state", state, "--token-file", tokenFile, "--node-id", "demo-0", "--labels", "namespace=labs,app=demo,pod=demo-0", "--axlr-bin", bin, "--heartbeat", "50ms", "--poll", "100ms", "--enroll-retry", "20ms"}
	serial := ""
	for _, want := range []string{"enrolled as node demo-0 with token " + tokenID, "keeping the certificate for node demo-0 (token " + tokenID + " already used)"} {
		var stdout bytes.Buffer
		var stderr syncBuffer
		done := make(chan int, 1)
		go func() { done <- run(args, &stdout, &stderr) }()
		deadline := time.Now().Add(5 * time.Second)
		for {
			node, err := operator.GetNode(context.Background(), "demo-0")
			if err == nil && node.Status == "Online" && node.Labels["app"] == "demo" && node.Labels["zone"] == "k8s" && strings.Contains(stderr.String(), "registered as demo-0") {
				if serial != "" && node.CertificateSerial != serial {
					t.Fatalf("the restart re-enrolled: %s then %s", serial, node.CertificateSerial)
				}
				serial = node.CertificateSerial
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("node not online: %+v %v\n%s", node, err, stderr.String())
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
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, stderr.String())
		}
	}
	// Stopped while it waits for a token: a clean exit.
	var stdout bytes.Buffer
	var stderr syncBuffer
	done := make(chan int, 1)
	go func() {
		done <- run([]string{"--control", url, "--state", filepath.Join(t.TempDir(), "w"), "--token-file", filepath.Join(t.TempDir(), "none"), "--enroll-retry", "20ms"}, &stdout, &stderr)
	}()
	for !strings.Contains(stderr.String(), "retrying") {
		time.Sleep(10 * time.Millisecond)
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	if code := <-done; code != exitOK {
		t.Errorf("stopped while waiting: %d", code)
	}
	// --token-file is for the TLS address only.
	var usage bytes.Buffer
	if code := run([]string{"--control", "unix:///x.sock", "--token-file", tokenFile}, &stdout, &usage); code != exitUsage || !strings.Contains(usage.String(), "--token-file needs") {
		t.Errorf("token file over the socket: %d %s", code, usage.String())
	}
}

func TestEnrollWithTokenFile(t *testing.T) {
	operator, url, _ := tlsControlPlane(t)
	tokenFile, _ := mintToFile(t, operator, "web-3")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"enroll", "--control", url, "--token-file", tokenFile, "--state", t.TempDir()}, &stdout, &stderr); code != exitOK || !strings.Contains(stdout.String(), `"nodeId": "web-3"`) {
		t.Fatalf("enroll --token-file: %d %s %s", code, stdout.String(), stderr.String())
	}
	stderr.Reset()
	if code := run([]string{"enroll", "--control", url, "--token-file", filepath.Join(t.TempDir(), "none")}, &stdout, &stderr); code != exitError {
		t.Errorf("missing token file: %d %s", code, stderr.String())
	}
}
