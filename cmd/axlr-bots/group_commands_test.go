package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestGroupCommands(t *testing.T) {
	c := cli{t: t, state: t.TempDir(), axlr: fakeAXLR(t, "completed"), socket: ""}
	for _, name := range []string{"bot-team-a.json", "bot-team-b.json"} {
		if code, _, errText := c.run("bot", "register", "--file", filepath.Join("..", "..", "examples", name)); code != exitOK {
			t.Fatalf("register %s: %s", name, errText)
		}
	}
	code, out, errText := c.run("group", "list")
	var groups []map[string]any
	if code != exitOK || json.Unmarshal([]byte(out), &groups) != nil || len(groups) != 1 || groups[0]["name"] != "releases" {
		t.Fatalf("group list: %d %s %s", code, out, errText)
	}
	if members := groups[0]["members"].([]any); len(members) != 2 || members[0].(map[string]any)["botId"] != "team-a" {
		t.Errorf("members = %v", members)
	}
	code, group := c.json("group", "get", "releases", "--tenant", "default")
	if code != exitOK || group["tenantId"] != "default" {
		t.Errorf("group get: %d %v", code, group)
	}
	if code, _, errText := c.run("group", "get", "missing"); code == exitOK || !strings.Contains(errText, "not found") {
		t.Errorf("missing group: %d %s", code, errText)
	}
	for _, args := range [][]string{{"group", "list", "extra"}, {"group", "get"}, {"group", "get", "--bogus"}} {
		if code, _, _ := c.run(args...); code != exitUsage {
			t.Errorf("%v: exit %d", args, code)
		}
	}
}

func TestGroupEventsCommand(t *testing.T) {
	c := cli{t: t, state: t.TempDir(), axlr: fakeAXLR(t, "completed"), socket: ""}
	if code, _, errText := c.run("bot", "register", "--file", filepath.Join("..", "..", "examples", "bot-team-a.json")); code != exitOK {
		t.Fatalf("register: %s", errText)
	}
	code, out, errText := c.run("group", "events", "releases", "--limit", "10")
	var events []map[string]any
	if code != exitOK || json.Unmarshal([]byte(out), &events) != nil || len(events) != 1 || events[0]["type"] != "bot.registered" {
		t.Fatalf("group events: %d %s %s", code, out, errText)
	}
	code, out, _ = c.run("group", "events", "releases", "--after", events[0]["eventId"].(string))
	if code != exitOK || strings.TrimSpace(out) != "[]" {
		t.Errorf("after = %d %s", code, out)
	}
	for _, args := range [][]string{{"group", "events"}, {"group", "events", "releases", "--bogus"}} {
		if code, _, _ := c.run(args...); code != exitUsage {
			t.Errorf("%v: exit %d", args, code)
		}
	}
	if code, _, _ := c.run("group", "events", "missing"); code == exitOK {
		t.Error("a missing team answered")
	}
}
