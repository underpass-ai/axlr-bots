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
		if code, _, errText := c.run("bot", "register", "--file", filepath.Join("testdata", name)); code != exitOK {
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
	if code, _, errText := c.run("bot", "register", "--file", filepath.Join("testdata", "bot-team-a.json")); code != exitOK {
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

func TestStopAndUrgentCommands(t *testing.T) {
	c := cli{t: t, state: t.TempDir(), axlr: fakeAXLR(t, "completed"), socket: ""}
	if code, _, errText := c.run("bot", "register", "--file", filepath.Join("testdata", "bot-team-b.json")); code != exitOK {
		t.Fatalf("register: %s", errText)
	}
	if code, _, errText := c.run("bot", "enable", "team-b"); code != exitOK {
		t.Fatalf("enable: %s", errText)
	}
	code, submitted := c.json("task", "submit", "team-b", "--objective", "Come back", "--key", "u-1", "--urgent")
	if code != exitOK || submitted["task"].(map[string]any)["priority"] != "urgent" {
		t.Fatalf("submit = %d %v", code, submitted)
	}
	code, stopped := c.json("bot", "stop", "team-b")
	if code != exitOK || len(stopped["cancelled"].([]any)) != 1 || stopped["bot"].(map[string]any)["status"] != "Paused" {
		t.Errorf("stop = %d %v", code, stopped)
	}
	if code, _, _ := c.run("bot", "stop"); code != exitUsage {
		t.Errorf("stop without id: %d", code)
	}
	if code, _, _ := c.run("bot", "stop", "ghost"); code == exitOK {
		t.Error("a missing bot was stopped")
	}
}

func TestGroupPublishAndTopicsCommands(t *testing.T) {
	c := cli{t: t, state: t.TempDir(), axlr: fakeAXLR(t, "completed"), socket: ""}
	for _, name := range []string{"bot-team-a.json", "bot-team-b.json"} {
		if code, _, errText := c.run("bot", "register", "--file", filepath.Join("testdata", name)); code != exitOK {
			t.Fatalf("register %s: %s", name, errText)
		}
	}
	for _, bot := range []string{"team-a", "team-b"} {
		if code, _, errText := c.run("bot", "enable", bot); code != exitOK {
			t.Fatalf("enable: %s", errText)
		}
	}
	code, answer := c.json("group", "publish", "releases", "stop.all", "--message", "Everyone stop", "--key", "p-1", "--data", `{"why": "a storm"}`)
	if code != exitOK || answer["created"] != true || len(answer["deliveries"].([]any)) != 2 {
		t.Fatalf("publish = %d %v", code, answer)
	}
	if code, again := c.json("group", "publish", "releases", "stop.all", "--message", "Everyone stop", "--key", "p-1", "--data", `{"why": "a storm"}`); code != exitOK || again["created"] != false {
		t.Errorf("again = %d %v", code, again)
	}
	if code, note := c.json("group", "publish", "releases", "info.turno", "--message", "shift", "--to", "team-a"); code != exitOK || note["deliveries"].([]any)[0].(map[string]any)["mode"] != "note" {
		t.Errorf("note = %d %v", code, note)
	}
	if code, _, errText := c.run("group", "publish", "releases", "error.bot", "--message", "x"); code == exitOK || !strings.Contains(errText, "not_allowed") {
		t.Errorf("error.bot = %d %s", code, errText)
	}
	code, out, errText := c.run("group", "topics", "releases", "--tenant", "default")
	var topics []map[string]any
	if code != exitOK || json.Unmarshal([]byte(out), &topics) != nil || len(topics) != 2 || topics[0]["name"] != "info.turno" || topics[1]["publishers"].([]any)[0] != "operator" {
		t.Fatalf("topics = %d %s %s", code, out, errText)
	}
	for _, args := range [][]string{{"group", "topics"}, {"group", "topics", "--bogus"}, {"group", "publish", "releases", "x"}, {"group", "publish", "releases", "x", "--message", "y", "--data", "[1]"}, {"group", "publish", "--bogus"}} {
		if code, _, _ := c.run(args...); code != exitUsage {
			t.Errorf("%v: exit %d", args, code)
		}
	}
	for _, args := range [][]string{{"group", "topics", "missing"}, {"group", "publish", "missing", "x", "--message", "y"}} {
		if code, _, _ := c.run(args...); code == exitOK {
			t.Errorf("%v answered", args)
		}
	}
}
