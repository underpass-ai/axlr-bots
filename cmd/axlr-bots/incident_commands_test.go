package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIncidentCommands(t *testing.T) {
	c := cli{t: t, state: t.TempDir(), axlr: fakeAXLR(t, "completed"), socket: ""}
	definition := filepath.Join(t.TempDir(), "triage.json")
	_ = os.WriteFile(definition, []byte(`{"id": "triage", "tenant": "ops", "name": "triage", "owner": "sre", "mission": "Find the cause of each incident with evidence.", "allowedTools": ["local_read"], "triggers": [{"type": "incident"}], "model": {"model": "anthropic/claude-haiku-5.5"}, "budgets": {"taskTimeoutSeconds": 60}}`), 0o600)
	if code, _, errText := c.run("bot", "register", "--file", definition); code != exitOK {
		t.Fatalf("register: %s", errText)
	}
	if code, _, errText := c.run("bot", "enable", "triage"); code != exitOK {
		t.Fatalf("enable: %s", errText)
	}
	open := []string{"incident", "open", "--tenant", "ops", "--bot", "triage", "--group-key", `{}/{alertname="KubePodCrashLooping"}`, "--title", "KubePodCrashLooping in shop", "--severity", "sev2", "--fingerprints", "fp-a, fp-b", "--source", "alertmanager"}
	code, opened := c.json(open...)
	if code != exitOK || opened["created"] != true || opened["task"] == nil {
		t.Fatalf("open = %d %v", code, opened)
	}
	incident := opened["incident"].(map[string]any)
	id := incident["id"].(string)
	if incident["status"] != "Open" || len(incident["fingerprints"].([]any)) != 2 || incident["severity"] != "sev2" {
		t.Errorf("incident = %v", incident)
	}
	code, again := c.json(open...)
	if code != exitOK || again["created"] != false || again["task"] != nil || again["incident"].(map[string]any)["repeats"] != float64(1) {
		t.Errorf("repeat = %d %v", code, again)
	}
	code, out, errText := c.run("incident", "list", "--tenant", "ops", "--status", "Open")
	var listed []map[string]any
	if code != exitOK || json.Unmarshal([]byte(out), &listed) != nil || len(listed) != 1 || listed[0]["id"] != id {
		t.Fatalf("list = %d %s %s", code, out, errText)
	}
	task := opened["task"].(map[string]any)["id"].(string)
	if code, _, errText := c.run("task", "run", task); code != exitOK {
		t.Fatalf("run: %d %s", code, errText)
	}
	code, got := c.json("incident", "get", id)
	if code != exitOK || got["status"] != "Diagnosed" || got["diagnosis"] == "" || got["taskIds"].([]any)[0] != task {
		t.Errorf("get = %d %v", code, got)
	}
	code, resolved := c.json("incident", "resolve", id, "--reason", "fixed")
	if code != exitOK || resolved["status"] != "Resolved" || resolved["reason"] != "fixed" {
		t.Errorf("resolve = %d %v", code, resolved)
	}
	if code, _, errText := c.run("incident", "resolve", id); code == exitOK || !strings.Contains(errText, "Resolved") {
		t.Errorf("resolve twice: %d %s", code, errText)
	}
	if code, _, errText := c.run("incident", "get", "inc-ghost"); code == exitOK || !strings.Contains(errText, "not found") {
		t.Errorf("ghost: %d %s", code, errText)
	}
	for _, args := range [][]string{
		{"incident", "open", "--tenant", "ops"}, {"incident", "open", "extra", "--tenant", "ops", "--bot", "triage", "--group-key", "g", "--title", "t"},
		{"incident", "open", "--bogus"}, {"incident", "list", "extra"}, {"incident", "list", "--bogus"},
		{"incident", "get"}, {"incident", "resolve"}, {"incident", "resolve", "--bogus"},
	} {
		if code, _, _ := c.run(args...); code != exitUsage {
			t.Errorf("%v: exit %d", args, code)
		}
	}
	if code, _, _ := c.run("incident", "list", "--status", "Closed"); code == exitOK {
		t.Error("a bad status listed")
	}
}
