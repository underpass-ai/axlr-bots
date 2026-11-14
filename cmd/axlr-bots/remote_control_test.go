package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/grpcclient"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
)

// remoteCLI runs commands with --control: the operator address, its
// token and the fleet CA, no socket and a state directory that must stay
// untouched.
type remoteCLI struct {
	t       *testing.T
	control string
	token   string
	ca      string
	state   string
	// certFile and keyFile are the operator's certificate, added as
	// --cert-file and --key-file when set.
	certFile, keyFile string
	// transport is added as --transport when set; gRPC is the default.
	transport string
}

func (c remoteCLI) run(args ...string) (int, string, string) {
	c.t.Helper()
	var stdout, stderr bytes.Buffer
	global := []string{"--state", c.state, "--socket", "", "--actor", "test:remote", "--control", c.control, "--token-file", c.token, "--ca-file", c.ca}
	if c.certFile != "" {
		global = append(global, "--cert-file", c.certFile, "--key-file", c.keyFile)
	}
	if c.transport != "" {
		global = append(global, "--transport", c.transport)
	}
	code := run(append(global, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	return address
}

// TestCommandsReachARemoteControlPlane starts serve with an operator
// address and drives it from "another host": a state directory of its
// own that the commands never open, the token and the CA as files.
func TestCommandsReachARemoteControlPlane(t *testing.T) {
	state := t.TempDir()
	socket := filepath.Join(t.TempDir(), "cp.sock")
	fake := fakeAXLR(t, "completed")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--state", state, "ca", "init"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("ca init: %s", stderr.String())
	}
	var ca struct {
		Certificate string `json:"certificate"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &ca); err != nil || ca.Certificate == "" {
		t.Fatalf("ca init answered %s (%v)", stdout.String(), err)
	}
	api := freeAddress(t)
	grpcAddr := freeAddress(t)
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan int, 1)
	var serveOut, serveErr syncBuffer
	go func() {
		done <- serve(ctx, mustApp(t, state, fake), state, socket, []string{"--api-listen", api, "--grpc-listen", grpcAddr, "--tick", "20ms"}, &serveOut, &serveErr)
	}()
	defer func() { stop(); <-done }()
	for deadline := time.Now().Add(5 * time.Second); !unixapi.Reachable(context.Background(), socket) || !grpcclient.Reachable(context.Background(), filepath.Join(state, grpcSocketFileName)); {
		if time.Now().After(deadline) {
			t.Fatalf("daemon not reachable: %s", serveErr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}

	elsewhere := t.TempDir()
	caFile := filepath.Join(elsewhere, "ca.crt")
	_ = os.WriteFile(caFile, []byte(ca.Certificate), 0o644)
	token, err := os.ReadFile(filepath.Join(state, apiTokenFileName))
	if err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(elsewhere, "token")
	_ = os.WriteFile(tokenFile, token, 0o600)
	localState := filepath.Join(elsewhere, "state")
	// The operator's certificate, issued on the control plane's host and
	// copied here.
	operatorDir := filepath.Join(elsewhere, "operator")
	var issuedOut bytes.Buffer
	if code := run([]string{"--state", state, "ca", "operator", "issue", "alice", "--out", operatorDir}, &issuedOut, &stderr); code != exitOK {
		t.Fatalf("ca operator issue: %s", stderr.String())
	}
	certFile, keyFile := filepath.Join(operatorDir, operatorCertFile), filepath.Join(operatorDir, operatorKeyFile)
	c := remoteCLI{t: t, control: grpcAddr, token: tokenFile, ca: caFile, state: localState, certFile: certFile, keyFile: keyFile}
	// Without a certificate the operator address refuses the connection,
	// and the CLI says how to get one before dialling.
	bare := remoteCLI{t: t, control: grpcAddr, token: tokenFile, ca: caFile, state: localState}
	t.Setenv("AXLR_BOTS_OPERATOR_DIR", filepath.Join(elsewhere, "nowhere"))
	if code, _, errText := bare.run("bot", "list"); code == exitOK || !strings.Contains(errText, "ca operator issue") {
		t.Errorf("no certificate: %d %s", code, errText)
	}
	// ... and finds it where ca operator issue leaves it.
	t.Setenv("AXLR_BOTS_OPERATOR_DIR", operatorDir)
	if code, _, errText := bare.run("bot", "list"); code != exitOK {
		t.Errorf("the default location: %d %s", code, errText)
	}
	// A node's certificate is no operator's.
	nodeDir := filepath.Join(elsewhere, "node")
	if code := run([]string{"--state", state, "ca", "issue-client", "--name", "alice", "--out", nodeDir}, &stdout, &stderr); code != exitOK {
		t.Fatalf("issue-client: %s", stderr.String())
	}
	service := remoteCLI{t: t, control: grpcAddr, token: tokenFile, ca: caFile, state: localState, certFile: filepath.Join(nodeDir, "client.crt"), keyFile: filepath.Join(nodeDir, "client.key")}
	if code, _, errText := service.run("bot", "list"); code == exitOK || !strings.Contains(errText, "not issued to an operator") {
		t.Errorf("a service certificate: %d %s", code, errText)
	}
	// The HTTP operator address stays served until it is removed.
	rest := remoteCLI{t: t, control: "https://" + api, token: tokenFile, ca: caFile, state: localState, transport: transportREST}
	if code, out, errText := rest.run("health"); code != exitOK || strings.TrimSpace(out) != "ok" {
		t.Fatalf("rest health: %d %s %s", code, out, errText)
	}

	if code, out, errText := c.run("health"); code != exitOK || strings.TrimSpace(out) != "ok" {
		t.Fatalf("health: %d %s %s", code, out, errText)
	}
	if code, out, errText := c.run("bot", "register", "--file", sreDefinitionFile(t)); code != exitOK || !strings.Contains(out, `"id": "bot"`) {
		t.Fatalf("register: %d %s %s", code, out, errText)
	}
	if code, _, errText := c.run("bot", "enable", "bot"); code != exitOK {
		t.Fatalf("enable: %s", errText)
	}
	code, out, errText := c.run("task", "submit", "bot", "--objective", "say hello", "--key", "remote-1")
	if code != exitOK {
		t.Fatalf("submit: %d %s %s", code, out, errText)
	}
	var submitted struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	if err := json.Unmarshal([]byte(out), &submitted); err != nil || submitted.Task.ID == "" {
		t.Fatalf("submit answered %s (%v)", out, err)
	}
	// The daemon ran it: the task is the daemon's, read back over the address.
	for deadline := time.Now().Add(5 * time.Second); ; {
		_, out, _ := c.run("task", "get", submitted.Task.ID)
		if strings.Contains(out, `"status": "Completed"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task not run: %s\n%s", out, serveErr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(localState); !os.IsNotExist(err) {
		t.Errorf("a command with --control opened the local state: %v", err)
	}

	// The CLI acts as the certificate's operator: a decision is recorded
	// as alice, and the control plane refuses any other actor.
	var decided, decidedErr bytes.Buffer
	args := []string{"--state", localState, "--socket", "", "--actor", "someone-else", "--control", grpcAddr, "--token-file", tokenFile, "--ca-file", caFile, "--cert-file", certFile, "--key-file", keyFile, "task", "decide", submitted.Task.ID, "approve"}
	if code := run(args, &decided, &decidedErr); code == exitOK || !strings.Contains(decidedErr.String(), "someone-else") {
		t.Errorf("another actor than the certificate's: %d %s", code, decidedErr.String())
	}

	// Revoked, the operator is refused from the next call on.
	if code, out, errText := c.run("bot", "list"); code != exitOK {
		t.Fatalf("before revoking: %d %s %s", code, out, errText)
	}
	if code := run([]string{"--state", state, "ca", "operator", "revoke", "--cert-file", certFile}, &stdout, &stderr); code != exitOK {
		t.Fatalf("ca operator revoke: %s", stderr.String())
	}
	if code, _, errText := c.run("bot", "list"); code == exitOK || !strings.Contains(strings.ToLower(errText), "revoked") {
		t.Errorf("a revoked certificate: %d %s", code, errText)
	}
	fresh := filepath.Join(elsewhere, "operator-2")
	if code := run([]string{"--state", state, "ca", "operator", "issue", "alice", "--out", fresh}, &stdout, &stderr); code != exitOK {
		t.Fatalf("ca operator issue again: %s", stderr.String())
	}
	again := remoteCLI{t: t, control: grpcAddr, token: tokenFile, ca: caFile, state: localState, certFile: filepath.Join(fresh, operatorCertFile), keyFile: filepath.Join(fresh, operatorKeyFile)}
	if code, _, errText := again.run("bot", "list"); code != exitOK {
		t.Errorf("a new certificate for the same operator: %d %s", code, errText)
	}
	c = again

	// A wrong token is refused, a wrong CA fails the handshake.
	_ = os.WriteFile(tokenFile, []byte("not-the-token\n"), 0o600)
	if code, _, errText := c.run("bot", "list"); code == exitOK || !strings.Contains(strings.ToLower(errText), "unauthorized") {
		t.Errorf("a wrong token: %d %s", code, errText)
	}
	_ = os.WriteFile(tokenFile, token, 0o600)
	otherState := t.TempDir()
	if code := run([]string{"--state", otherState, "ca", "init"}, &stdout, &stderr); code != exitOK {
		t.Fatal("second ca")
	}
	otherCA, _ := os.ReadFile(filepath.Join(otherState, "ca", "ca.crt"))
	_ = os.WriteFile(caFile, otherCA, 0o644)
	if code, _, errText := c.run("bot", "list"); code == exitOK || !strings.Contains(errText, "certificate") {
		t.Errorf("another CA: %d %s", code, errText)
	}
}

func TestControlFlagsAreChecked(t *testing.T) {
	for _, args := range [][]string{
		{"--control", "http://127.0.0.1:7879", "--token-file", "/dev/null", "bot", "list"},
		{"--control", "https://127.0.0.1:7879/v1", "--token-file", "/dev/null", "bot", "list"},
		{"--control", "https://127.0.0.1:7879", "bot", "list"},
		{"--token-file", "/dev/null", "bot", "list"},
		{"--ca-file", "/dev/null", "bot", "list"},
		{"--cert-file", "/dev/null", "bot", "list"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(append([]string{"--state", t.TempDir()}, args...), &stdout, &stderr); code != exitUsage {
			t.Errorf("%v: %d %s", args, code, stderr.String())
		}
	}
	empty := filepath.Join(t.TempDir(), "token")
	_ = os.WriteFile(empty, []byte("\n"), 0o600)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--state", t.TempDir(), "--control", "https://127.0.0.1:1", "--token-file", empty, "bot", "list"}, &stdout, &stderr); code == exitOK || !strings.Contains(stderr.String(), "is empty") {
		t.Errorf("an empty token: %d %s", code, stderr.String())
	}
}
