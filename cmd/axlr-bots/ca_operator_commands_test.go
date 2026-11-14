package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/adapters/filestore"
)

func TestCAOperatorIssueAndRevoke(t *testing.T) {
	state := t.TempDir()
	c := cli{t: t, state: state, axlr: "/nonexistent/axlr-tui"}
	if code, _, _ := c.run("ca", "operator", "issue", "alice"); code != exitError {
		t.Errorf("without a CA: %d", code)
	}
	if code, _ := c.json("ca", "init"); code != exitOK {
		t.Fatal("ca init")
	}
	out := filepath.Join(t.TempDir(), "operator")
	code, report := c.json("ca", "operator", "issue", "alice", "--out", out, "--lifetime", "48h")
	if code != exitOK || report["name"] != "alice" || report["serial"] == "" || report["dir"] != out {
		t.Fatalf("issue: %d %v", code, report)
	}
	for file, mode := range map[string]os.FileMode{operatorKeyFile: 0o600, operatorCertFile: 0o644, operatorCAFile: 0o644} {
		info, err := os.Stat(filepath.Join(out, file))
		if err != nil || info.Mode().Perm() != mode {
			t.Errorf("%s: %v %v", file, info, err)
		}
	}
	if code, _, errText := c.run("ca", "operator", "issue", "alice", "--out", out); code != exitError || !strings.Contains(errText, "exists") {
		t.Errorf("overwrite: %d %s", code, errText)
	}
	for _, args := range [][]string{
		{"ca", "operator"}, {"ca", "operator", "bogus"}, {"ca", "operator", "issue"}, {"ca", "operator", "issue", "a b"},
		{"ca", "operator", "issue", "a", "b"}, {"ca", "operator", "issue", "alice", "--bogus"},
		{"ca", "operator", "revoke"}, {"ca", "operator", "revoke", "--cert-file", "x", "--serial", "1"}, {"ca", "operator", "revoke", "x"},
		{"ca", "operator", "revoke", "--bogus"},
	} {
		if code, _, _ := c.run(args...); code != exitUsage {
			t.Errorf("%v: %d", args, code)
		}
	}

	// Revoking by certificate or by serial lands on the denylist the
	// daemon reads; a node's or a service's certificate is not revoked as
	// an operator's.
	denylist := filestore.NewCertificateDenylist(state)
	if code, revoked := c.json("ca", "operator", "revoke", "--cert-file", filepath.Join(out, operatorCertFile)); code != exitOK || revoked["serial"] != report["serial"] || revoked["name"] != "alice" {
		t.Fatalf("revoke by certificate: %d %v", code, revoked)
	}
	if denied, _ := denylist.IsDenied(t.Context(), report["serial"].(string)); !denied {
		t.Error("the serial is not on the denylist")
	}
	if code, _ := c.json("ca", "operator", "revoke", "--serial", "ABCDEF", "--name", "bob"); code != exitOK {
		t.Error("revoke by serial")
	}
	if denied, _ := denylist.IsDenied(t.Context(), "abcdef"); !denied {
		t.Error("the serial is kept in lower case")
	}
	if code, _, errText := c.run("ca", "operator", "revoke", "--cert-file", filepath.Join(t.TempDir(), "missing")); code != exitError {
		t.Errorf("a missing certificate: %d %s", code, errText)
	}
	nodeDir := filepath.Join(t.TempDir(), "service")
	if code, _ := c.json("ca", "issue-client", "--name", "alice", "--out", nodeDir); code != exitOK {
		t.Fatal("issue-client")
	}
	if code, _, errText := c.run("ca", "operator", "revoke", "--cert-file", filepath.Join(nodeDir, "client.crt")); code != exitError || !strings.Contains(errText, "not issued to an operator") {
		t.Errorf("a service certificate: %d %s", code, errText)
	}
}

func TestTheOperatorsCertificateIsFoundWhereIssueLeavesIt(t *testing.T) {
	state := t.TempDir()
	c := cli{t: t, state: state, axlr: "/nonexistent/axlr-tui"}
	c.json("ca", "init")
	dir := filepath.Join(t.TempDir(), "operator")
	t.Setenv("AXLR_BOTS_OPERATOR_DIR", dir)
	if defaultOperatorDir() != dir {
		t.Errorf("AXLR_BOTS_OPERATOR_DIR: %s", defaultOperatorDir())
	}
	if _, ok := (remoteControl{}).operatorName(); ok {
		t.Error("a name with no certificate")
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--state", state, "ca", "operator", "issue", "carol"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("issue to the default: %s", stderr.String())
	}
	var report map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil || report["dir"] != dir {
		t.Fatalf("the default directory: %v %s", err, stdout.String())
	}
	if name, ok := (remoteControl{}).operatorName(); !ok || name != "carol" {
		t.Errorf("operatorName = %q %v", name, ok)
	}
	t.Setenv("AXLR_BOTS_OPERATOR_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if got := defaultOperatorDir(); got == dir || got == "" {
		t.Errorf("fallback: %q", got)
	}
}
