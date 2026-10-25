package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/internal/fakeaxlr"
)

func TestADecisionNamesItsCall(t *testing.T) {
	dir := t.TempDir()
	bin, _, err := fakeaxlr.Write(dir, fakeaxlr.Options{Scenario: "approval-flow", Marker: filepath.Join(dir, "marker")})
	if err != nil {
		t.Fatal(err)
	}
	c := cli{t: t, state: t.TempDir(), axlr: bin}
	_, _ = c.json("bot", "register", "--file", filepath.Join("..", "..", "examples", "bot-act-with-approval.json"))
	_, _ = c.json("bot", "enable", "bot-act-with-approval")
	_, submitted := c.json("task", "submit", "bot-act-with-approval", "--objective", "Create hello.txt", "--key", "w-1")
	taskID := submitted["task"].(map[string]any)["id"].(string)
	if code, ran := c.json("task", "run", taskID); code != exitDecisionRequired {
		t.Fatalf("run: %d %v", code, ran)
	}
	approval := func() map[string]any {
		_, task := c.json("task", "get", taskID)
		approval, _ := task["approval"].(map[string]any)
		return approval
	}
	if a := approval(); a["callId"] != "c1" || a["onExpire"] != "deny" || a["deadline"] == nil {
		t.Errorf("approval = %v", a)
	}
	if code, _, errText := c.run("task", "approve", taskID, "--call", "c9"); code != exitError || !strings.Contains(errText, "waiting on call c1") {
		t.Errorf("another call: %d %s", code, errText)
	}
	for _, usage := range [][]string{{"task", "approve"}, {"task", "approve", taskID, "--nope"}, {"task", "decide", taskID}, {"task", "decide", taskID, "maybe"}, {"task", "decide", "--call"}} {
		if code, _, _ := c.run(usage...); code != exitUsage {
			t.Errorf("%v: %d", usage, code)
		}
	}
	if code, decided := c.json("task", "decide", "--call", "c1", taskID, "approve"); code != exitOK || decided["task"].(map[string]any)["status"] != "Completed" {
		t.Errorf("decide: %d %v", code, decided)
	}
	if a := approval(); a["outcome"] != "approved" {
		t.Errorf("approval after = %v", a)
	}
}

func TestExecutionArtifactPrintsWhatTheControlPlaneKeeps(t *testing.T) {
	dir := t.TempDir()
	bin, _, err := fakeaxlr.Write(dir, fakeaxlr.Options{Scenario: "delivered"})
	if err != nil {
		t.Fatal(err)
	}
	c := cli{t: t, state: t.TempDir(), axlr: bin}
	_, _ = c.json("bot", "register", "--file", filepath.Join("..", "..", "examples", "bot.json"))
	_, _ = c.json("bot", "enable", "bot")
	_, submitted := c.json("task", "submit", "bot", "--objective", "Write the notes", "--key", "n-1")
	taskID := submitted["task"].(map[string]any)["id"].(string)
	code, ran := c.json("task", "run", taskID)
	if code != exitOK {
		t.Fatalf("run: %d %v", code, ran)
	}
	execution := ran["execution"].(map[string]any)
	executionID := execution["id"].(string)
	artifacts := execution["result"].(map[string]any)["artifacts"].([]any)
	if len(artifacts) != 3 || artifacts[0].(map[string]any)["stored"] != true || artifacts[1].(map[string]any)["stored"] != nil {
		t.Errorf("artifacts = %v", artifacts)
	}
	if execution["receipt"] == nil {
		t.Error("the receipt was not kept")
	}
	if code, out, _ := c.run("execution", "artifact", executionID, "notes.md"); code != exitOK || out != fakeaxlr.DeliveredContent {
		t.Errorf("stdout: %d %q", code, out)
	}
	file := filepath.Join(t.TempDir(), "notes.md")
	for _, args := range [][]string{{"execution", "artifact", "--out", file, executionID, "notes.md"}, {"execution", "artifact", executionID, "notes.md", "--out", file + "2"}} {
		if code, out, _ := c.run(args...); code != exitOK || !strings.Contains(out, "text/markdown") {
			t.Errorf("%v: %d %s", args, code, out)
		}
	}
	for _, path := range []string{file, file + "2"} {
		if data, _ := os.ReadFile(path); string(data) != fakeaxlr.DeliveredContent {
			t.Errorf("%s = %q", path, data)
		}
	}
	if code, _, errText := c.run("execution", "artifact", executionID, "plan.md"); code != exitError || !strings.Contains(errText, "not found") {
		t.Errorf("not kept: %d %s", code, errText)
	}
	if code, _, _ := c.run("execution", "artifact", executionID); code != exitUsage {
		t.Errorf("usage: %d", code)
	}
	if code, _, _ := c.run("execution", "artifact", "--bad"); code != exitUsage {
		t.Errorf("bad flag: %d", code)
	}
	if code, _, _ := c.run("execution", "artifact", "--out", filepath.Join(dir, "missing", "x"), executionID, "notes.md"); code != exitError {
		t.Errorf("unwritable out: %d", code)
	}
}

func TestOperatorURL(t *testing.T) {
	host, _ := os.Hostname()
	for _, c := range []struct{ explicit, listen, want string }{
		{"https://cp.example.com:7879/", "127.0.0.1:7879", "https://cp.example.com:7879"},
		{"", "127.0.0.1:7879", "https://127.0.0.1:7879"},
		{"", "0.0.0.0:7879", "https://" + host + ":7879"},
		{"", "", ""},
		{"", "nonsense", ""},
	} {
		if got := operatorURL(c.explicit, c.listen); got != c.want {
			t.Errorf("operatorURL(%q, %q) = %q, want %q", c.explicit, c.listen, got, c.want)
		}
	}
}
