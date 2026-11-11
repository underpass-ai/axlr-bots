package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// The watcher of docs/reference/bot-definition.md registers as it is
// written there: a Jev bot on the reserved server axlr-bots, on a timer.
func TestTheExampleWatcherRegisters(t *testing.T) {
	c := cli{t: t, state: t.TempDir(), axlr: "/nonexistent/axlr-tui"}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	code, out, errText := c.run("bot", "register", "--file", filepath.Join("testdata", "bot-watcher.json"))
	if code != exitOK || !strings.Contains(out, `"server": "axlr-bots"`) || !strings.Contains(out, `"every": "5m0s"`) {
		t.Fatalf("register: %d %s %s", code, out, errText)
	}
}
