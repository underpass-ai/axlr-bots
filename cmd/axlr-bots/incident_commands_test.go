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
	// get adds the current task and the approvals; needsYou only when true.
	current, _ := got["currentTask"].(map[string]any)
	approvals, _ := got["approvals"].(map[string]any)
	if current["taskId"] != task || current["phase"] != "triage" || current["status"] != "Completed" || approvals["approved"] != float64(0) || got["needsYou"] != nil {
		t.Errorf("get's current task %v, approvals %v", current, approvals)
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

func TestPostmortemCommands(t *testing.T) {
	c := cli{t: t, state: t.TempDir(), axlr: fakeAXLR(t, "completed"), socket: ""}
	dir := t.TempDir()
	for _, bot := range []struct{ name, triggers string }{
		{"triage", `[{"type": "incident"}]`},
		{"scribe", `[{"type": "incident", "phase": "postmortem"}]`},
	} {
		file := filepath.Join(dir, bot.name+".json")
		_ = os.WriteFile(file, []byte(`{"id": "`+bot.name+`", "tenant": "ops", "name": "`+bot.name+`", "owner": "sre", "groups": ["sre"], "mission": "Keep the incident's record.", "allowedTools": ["local_read"], "triggers": `+bot.triggers+`, "model": {"model": "anthropic/claude-haiku-5.5"}, "budgets": {"taskTimeoutSeconds": 60}}`), 0o600)
		if code, _, errText := c.run("bot", "register", "--file", file); code != exitOK {
			t.Fatalf("register: %s", errText)
		}
		if code, _, errText := c.run("bot", "enable", bot.name); code != exitOK {
			t.Fatalf("enable: %s", errText)
		}
	}
	_, opened := c.json("incident", "open", "--tenant", "ops", "--bot", "triage", "--group-key", "g", "--title", "Disk full", "--severity", "sev1")
	id := opened["incident"].(map[string]any)["id"].(string)
	if code, _, errText := c.run("incident", "postmortem", "show", id); code != exitError || !strings.Contains(errText, "no postmortem") {
		t.Errorf("show before: %d %s", code, errText)
	}
	code, resolved := c.json("incident", "resolve", id)
	if code != exitOK {
		t.Fatalf("resolve = %d %v", code, resolved)
	}
	task := resolved["postmortem"].(map[string]any)["taskId"].(string)
	if code, _, errText := c.run("task", "run", task); code != exitOK {
		t.Fatalf("run: %s", errText)
	}
	code, out, errText := c.run("incident", "postmortem", "show", id)
	if code != exitOK || strings.TrimSpace(out) == "" || !strings.Contains(errText, "Draft") {
		t.Errorf("show = %d %q %q", code, out, errText)
	}
	code, back := c.json("--actor", "tirso", "incident", "postmortem", "send-back", id, "--note", "add the owner")
	if code != exitOK || back["postmortem"].(map[string]any)["reviewedBy"] != "tirso" {
		t.Fatalf("send-back = %d %v", code, back)
	}
	rewrite := back["postmortem"].(map[string]any)["taskId"].(string)
	if code, _, errText := c.run("task", "run", rewrite); code != exitOK {
		t.Fatalf("run: %s", errText)
	}
	code, approved := c.json("--actor", "tirso", "incident", "postmortem", "approve", id)
	if code != exitOK || approved["postmortem"].(map[string]any)["approvedBy"] != "tirso" {
		t.Fatalf("approve = %d %v", code, approved)
	}
	if code, _, _ := c.run("incident", "postmortem", "approve", id); code == exitOK {
		t.Error("approved twice")
	}
	for _, args := range [][]string{
		{"incident", "postmortem"}, {"incident", "postmortem", "show"}, {"incident", "postmortem", "burn", id},
		{"incident", "postmortem", "send-back", id}, {"incident", "postmortem", "approve", id, "--bogus"},
	} {
		if code, _, _ := c.run(args...); code != exitUsage {
			t.Errorf("%v: exit %d", args, code)
		}
	}
	if code, _, _ := c.run("incident", "postmortem", "show", "inc-ghost"); code == exitOK {
		t.Error("ghost")
	}
}
