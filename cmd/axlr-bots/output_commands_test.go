package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOutputCommands(t *testing.T) {
	c := cli{t: t, state: t.TempDir(), axlr: fakeAXLR(t, "completed"), socket: ""}
	dir := t.TempDir()
	urlFile := filepath.Join(dir, "slack.url")
	_ = os.WriteFile(urlFile, []byte("https://hooks.slack.test/services/T000/B000/SECRETPATH\n"), 0o600)
	code, chat := c.json("output", "add", "chat", "--type", "slack", "--url-file", urlFile, "--events", "opened, resolved", "--severities", "sev1,sev2", "--tenants", "ops")
	if code != exitOK || chat["host"] != "hooks.slack.test" || len(chat["events"].([]any)) != 2 || chat["fingerprint"] == nil {
		t.Fatalf("add slack = %d %v", code, chat)
	}
	// The secret from stdin, the program after --: its own flags are not ours.
	stdin = strings.NewReader("a-webhook-secret-of-32-characters\n")
	t.Cleanup(func() { stdin = os.Stdin })
	if code, _, errText := c.run("output", "add", "hook", "--type", "webhook", "--url", "https://receiver.test/axlr", "--secret-file", "-"); code != exitOK {
		t.Fatalf("add webhook: %s", errText)
	}
	if code, relay := c.json("output", "add", "relay", "--type", "command", "--", "/usr/local/bin/relay", "--type", "teams", "--events", "x"); code != exitOK || relay["type"] != "command" {
		t.Fatalf("add command = %d %v", code, relay)
	}
	definition := filepath.Join(dir, "pager.json")
	_ = os.WriteFile(definition, []byte(`{"name": "pager", "type": "command", "command": ["/usr/local/bin/page"], "severities": ["sev1"]}`), 0o600)
	if code, _, errText := c.run("output", "add", "--file", definition); code != exitOK {
		t.Fatalf("add from a file: %s", errText)
	}
	code, out, errText := c.run("output", "list")
	var listed []map[string]any
	if code != exitOK || json.Unmarshal([]byte(out), &listed) != nil || len(listed) != 4 {
		t.Fatalf("list = %d %s %s", code, out, errText)
	}
	for _, secret := range []string{"SECRETPATH", "/services/", "a-webhook-secret", "/usr/local/bin", "teams"} {
		if strings.Contains(out, secret) {
			t.Errorf("the list shows %q:\n%s", secret, out)
		}
	}
	stored, _ := os.ReadFile(filepath.Join(c.state, "outputs.json"))
	if !strings.Contains(string(stored), `"--type",`) || !strings.Contains(string(stored), "SECRETPATH") {
		t.Errorf("stored = %s", stored)
	}
	if code, removed := c.json("output", "remove", "chat"); code != exitOK || removed["name"] != "chat" {
		t.Errorf("remove = %d %v", code, removed)
	}
	for name, args := range map[string][]string{
		"no name":        {"output", "add", "--type", "slack", "--url", "https://x.test/"},
		"two names":      {"output", "add", "a", "b", "--type", "slack"},
		"two stdins":     {"output", "add", "a", "--type", "webhook", "--url-file", "-", "--secret-file", "-"},
		"bad flag":       {"output", "add", "a", "--nope"},
		"list arguments": {"output", "list", "x"},
		"remove no name": {"output", "remove"},
		"remove two":     {"output", "remove", "a", "b"},
	} {
		if code, _, _ := c.run(args...); code != exitUsage {
			t.Errorf("%s: exit %d", name, code)
		}
	}
	for name, args := range map[string][]string{
		"bad type":      {"output", "add", "a", "--type", "email"},
		"taken":         {"output", "add", "hook", "--type", "slack", "--url", "https://x.test/"},
		"missing file":  {"output", "add", "a", "--file", filepath.Join(dir, "nope.json")},
		"missing url":   {"output", "add", "a", "--type", "slack", "--url-file", filepath.Join(dir, "nope.url")},
		"removed twice": {"output", "remove", "chat"},
	} {
		if code, _, _ := c.run(args...); code != exitError {
			t.Errorf("%s: exit %d", name, code)
		}
	}
	broken := filepath.Join(dir, "broken.json")
	_ = os.WriteFile(broken, []byte(`{"name": "x", "kind": "slack"}`), 0o600)
	if code, _, errText := c.run("output", "add", "--file", broken); code != exitError || !strings.Contains(errText, "unknown field") {
		t.Errorf("unknown field: %d %s", code, errText)
	}
	big := filepath.Join(dir, "big")
	_ = os.WriteFile(big, []byte(strings.Repeat("x", maxSecretFileBytes+1)), 0o600)
	if code, _, errText := c.run("output", "add", "a", "--type", "slack", "--url-file", big); code != exitError || !strings.Contains(errText, "8 KiB") {
		t.Errorf("big file: %d %s", code, errText)
	}
}
