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
}

func (c remoteCLI) run(args ...string) (int, string, string) {
	c.t.Helper()
	var stdout, stderr bytes.Buffer
	global := []string{"--state", c.state, "--socket", "", "--actor", "test:remote", "--control", c.control, "--token-file", c.token, "--ca-file", c.ca}
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
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan int, 1)
	var serveOut, serveErr syncBuffer
	go func() {
		done <- serve(ctx, mustApp(t, state, fake), state, socket, []string{"--api-listen", api, "--tick", "20ms"}, &serveOut, &serveErr)
	}()
	defer func() { stop(); <-done }()
	for deadline := time.Now().Add(5 * time.Second); !unixapi.Reachable(context.Background(), socket); {
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
	c := remoteCLI{t: t, control: "https://" + api, token: tokenFile, ca: caFile, state: localState}

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

	// The activity audit reads and publishes through the address too.
	store := filepath.Join(elsewhere, "audit")
	stdout.Reset()
	stderr.Reset()
	remote := remoteControl{URL: c.control, TokenFile: tokenFile, CAFile: caFile}
	if code := activityAuditServe(context.Background(), localState, "", remote, []string{"--team", "watchers", "--event", "activity.failure", "--store", store, "--once", "--dry-run"}, &stdout, &stderr); code != exitOK || !strings.Contains(stderr.String(), "through "+c.control) {
		t.Fatalf("activity-audit over the address: %d %s %s", code, stdout.String(), stderr.String())
	}

	// A wrong token is refused, a wrong CA fails the handshake.
	_ = os.WriteFile(tokenFile, []byte("not-the-token\n"), 0o600)
	if code, _, errText := c.run("bot", "list"); code == exitOK || !strings.Contains(strings.ToLower(errText), "unauthorized") {
		t.Errorf("a wrong token: %d %s", code, errText)
	}
	_ = os.WriteFile(tokenFile, token, 0o600)
	other := t.TempDir()
	if code := run([]string{"--state", other, "ca", "init"}, &stdout, &stderr); code != exitOK {
		t.Fatal("second ca")
	}
	otherCA, _ := os.ReadFile(filepath.Join(other, "ca", "ca.crt"))
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
