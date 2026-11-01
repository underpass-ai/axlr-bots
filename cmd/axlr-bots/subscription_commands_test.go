package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubscriptionCommands(t *testing.T) {
	c := cli{t: t, state: t.TempDir(), axlr: fakeAXLR(t, "completed"), socket: ""}
	for _, name := range []string{"bot-team-a.json", "bot-team-b.json"} {
		if code, _, errText := c.run("bot", "register", "--file", filepath.Join("testdata", name)); code != exitOK {
			t.Fatalf("register %s: %s", name, errText)
		}
	}
	code, out, errText := c.run("bot", "subscribe", "team-a", "pedido.*", "--from", "team-b, operator", "--mode", "task")
	var list []map[string]any
	if code != exitOK || json.Unmarshal([]byte(out), &list) != nil || len(list) != 1 || list[0]["topic"] != "pedido.*" || list[0]["mode"] != "task" || len(list[0]["from"].([]any)) != 2 {
		t.Fatalf("subscribe: %d %s %s", code, out, errText)
	}
	// The same pattern again replaces it; another adds one.
	if code, _, errText := c.run("bot", "subscribe", "team-a", "pedido.*"); code != exitOK {
		t.Fatalf("subscribe again: %s", errText)
	}
	if code, _, errText := c.run("bot", "subscribe", "team-a", "warn.*", "--mode", "message"); code != exitOK {
		t.Fatalf("subscribe warn: %s", errText)
	}
	code, out, _ = c.run("bot", "subscriptions", "team-a")
	list = nil
	if code != exitOK || json.Unmarshal([]byte(out), &list) != nil || len(list) != 2 || list[0]["mode"] != nil {
		t.Fatalf("subscriptions: %d %s", code, out)
	}
	if code, _, errText := c.run("bot", "subscribe", "team-a", "x", "--from", "stranger"); code == exitOK || !strings.Contains(errText, "not a teammate") {
		t.Errorf("stranger: %d %s", code, errText)
	}
	if code, out, errText := c.run("bot", "unsubscribe", "team-a", "pedido.*"); code != exitOK || !strings.Contains(out, "warn.*") || strings.Contains(out, "pedido") {
		t.Errorf("unsubscribe: %d %s %s", code, out, errText)
	}
	if code, _, errText := c.run("bot", "unsubscribe", "team-a", "missing"); code == exitOK || !strings.Contains(errText, "not found") {
		t.Errorf("unsubscribe missing: %d %s", code, errText)
	}
	if code, out, _ := c.run("bot", "unsubscribe", "team-a", "warn.*"); code != exitOK || strings.TrimSpace(out) != "[]" {
		t.Errorf("last unsubscribe: %d %s", code, out)
	}
	if code, out, _ := c.run("bot", "subscriptions", "team-b"); code != exitOK || strings.TrimSpace(out) != "[]" {
		t.Errorf("none: %d %s", code, out)
	}
	for _, args := range [][]string{{"bot", "subscribe", "team-a"}, {"bot", "subscribe", "team-a", "x", "--bogus"}, {"bot", "unsubscribe", "team-a"}, {"bot", "subscriptions"}} {
		if code, _, _ := c.run(args...); code != exitUsage {
			t.Errorf("%v: exit %d", args, code)
		}
	}
	for _, args := range [][]string{{"bot", "subscribe", "missing", "x"}, {"bot", "unsubscribe", "missing", "x"}, {"bot", "subscriptions", "missing"}} {
		if code, _, _ := c.run(args...); code == exitOK {
			t.Errorf("%v answered", args)
		}
	}
}
