package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBotStageApplyAndUnstage(t *testing.T) {
	c := cli{t: t, state: t.TempDir(), axlr: fakeAXLR(t, "completed"), socket: ""}
	if code, _, errText := c.run("bot", "register", "--file", triggeredBotFile(t)); code != exitOK {
		t.Fatalf("register: %d %s", code, errText)
	}
	patch := filepath.Join(t.TempDir(), "patch.json")
	if err := os.WriteFile(patch, []byte(`{"version": "1.1.0", "mission": "Say what each deploy changed.",
  "triggers": [{"type": "manual"}, {"type": "webhook", "name": "deploy", "objective": "Handle {{.type}}", "secret": "`+cliSecret+`"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, staged := c.json("bot", "stage", "bot", "--file", patch)
	if code != exitOK || staged["mission"] == "Say what each deploy changed." {
		t.Fatalf("stage: %d %v", code, staged)
	}
	if held, _ := staged["staged"].(map[string]any); held["version"] != "1.1.0" || held["stagedAt"] == nil || held["change"] != nil {
		t.Fatalf("staged = %v", staged["staged"])
	}
	code, out, _ := c.run("bot", "staged", "bot")
	if code != exitOK || !strings.Contains(out, `"version": "1.1.0"`) || strings.Contains(out, cliSecret) || !strings.Contains(out, "Say what each deploy changed.") {
		t.Fatalf("staged: %d %s", code, out)
	}
	code, applied := c.json("bot", "apply-staged", "bot")
	if code != exitOK || applied["version"] != "1.1.0" || applied["staged"] != nil || applied["mission"] != "Say what each deploy changed." {
		t.Fatalf("apply-staged: %d %v", code, applied)
	}
	for _, args := range [][]string{{"bot", "apply-staged", "bot"}, {"bot", "unstage", "bot"}, {"bot", "staged", "bot"}} {
		if code, _, errText := c.run(args...); code != exitError || !strings.Contains(errText, "without a staged change") {
			t.Errorf("%v with nothing staged: %d %s", args, code, errText)
		}
	}
	if code, _ := c.json("bot", "stage", "bot", "--file", patch); code != exitOK {
		t.Fatal("stage again")
	}
	if code, unstaged := c.json("bot", "unstage", "bot"); code != exitOK || unstaged["staged"] != nil || unstaged["version"] != "1.1.0" {
		t.Fatalf("unstage: %d %v", code, unstaged)
	}

	noVersion := filepath.Join(t.TempDir(), "no-version.json")
	if err := os.WriteFile(noVersion, []byte(`{"mission": "m"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(t.TempDir(), "unknown.json")
	if err := os.WriteFile(unknown, []byte(`{"version": "1.2.0", "colour": "blue"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		args []string
		code int
		text string
	}{
		{[]string{"bot", "stage"}, exitUsage, "needs <bot-id> first"},
		{[]string{"bot", "stage", "--file", patch}, exitUsage, "needs <bot-id> first"},
		{[]string{"bot", "stage", "bot"}, exitUsage, "needs --file"},
		{[]string{"bot", "stage", "bot", "--bogus"}, exitUsage, "flag provided but not defined"},
		{[]string{"bot", "stage", "bot", "--file", filepath.Join(t.TempDir(), "none.json")}, exitError, "no such file"},
		{[]string{"bot", "stage", "bot", "--file", unknown}, exitError, "unknown field"},
		{[]string{"bot", "stage", "bot", "--file", noVersion}, exitError, "version"},
		{[]string{"bot", "stage", "ghost", "--file", patch}, exitError, "not found"},
		{[]string{"bot", "staged"}, exitUsage, "exactly one"},
		{[]string{"bot", "apply-staged", "a", "b"}, exitUsage, "exactly one"},
		{[]string{"bot", "apply-staged", "ghost"}, exitError, "not found"},
		{[]string{"bot", "unstage"}, exitUsage, "exactly one"},
		{[]string{"bot", "unstage", "ghost"}, exitError, "not found"},
	}
	for _, tc := range cases {
		code, _, errText := c.run(tc.args...)
		if code != tc.code || !strings.Contains(errText, tc.text) {
			t.Errorf("%v: exit %d stderr %q; want %d containing %q", tc.args, code, errText, tc.code, tc.text)
		}
	}
}

func TestServeRefusesAMinimumAXLRThatIsNoVersion(t *testing.T) {
	c := cli{t: t, state: t.TempDir(), axlr: fakeAXLR(t, "completed"), socket: filepath.Join(t.TempDir(), "cp.sock")}
	if code, _, errText := c.run("serve", "--min-axlr-version", "v0.20"); code != exitUsage || !strings.Contains(errText, "semantic version") {
		t.Fatalf("serve --min-axlr-version v0.20: %d %s", code, errText)
	}
}
