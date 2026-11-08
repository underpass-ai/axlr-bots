package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/internal/fakeaxlr"
)

// TestTheWriteNeverRunsWithoutTheApproval is the approval path of vision
// §10.4: a bot at act-with-approval asks AXLR for a write, AXLR holds the
// call and the task waits; the write runs only after `task approve`,
// through `--session <id> --decide approve`.
func TestTheWriteNeverRunsWithoutTheApproval(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "restart-ran")
	bin, argsFile, err := fakeaxlr.Write(dir, fakeaxlr.Options{Scenario: "approval-flow", Marker: marker})
	if err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	c := cli{t: t, state: state, axlr: bin}
	code, bot := c.json("bot", "register", "--file", filepath.Join("testdata", "bot-act-with-approval.json"))
	if code != exitOK || bot["level"] != "act-with-approval" {
		t.Fatalf("register: %d %v", code, bot)
	}
	_, _ = c.json("bot", "enable", "bot-act-with-approval")
	_, submitted := c.json("task", "submit", "bot-act-with-approval", "--objective", "Create a file named hello.txt in your workspace containing hello", "--key", "write-1")
	taskID := submitted["task"].(map[string]any)["id"].(string)

	code, ran := c.json("task", "run", taskID)
	if code != exitDecisionRequired || ran["task"].(map[string]any)["status"] != "WaitingApproval" {
		t.Fatalf("run: %d %v", code, ran)
	}
	execution := ran["execution"].(map[string]any)
	pending := execution["result"].(map[string]any)["pendingDecision"].(map[string]any)
	if pending["tool"] != "local_exec" || pending["callId"] != "c1" || pending["sessionId"] != "sess-1" {
		t.Errorf("pending = %v", pending)
	}
	policy := execution["policy"].(map[string]any)
	if policy["level"] != "act-with-approval" || policy["runtimeLevel"] != "act-with-approval" || policy["enforced"] != true {
		t.Errorf("policy = %v", policy)
	}
	raw, _ := os.ReadFile(argsFile)
	args := strings.Join(strings.Split(strings.TrimSpace(string(raw)), "\n"), " ")
	if !strings.Contains(args, "--approve none ") || !strings.Contains(args, "--policy "+policy["path"].(string)) {
		t.Errorf("first run args = %s", args)
	}
	var file struct {
		Policy struct {
			Level string `json:"level"`
			Tools struct {
				Allow []map[string]any `json:"allow"`
				Deny  []map[string]any `json:"deny"`
			} `json:"tools"`
		} `json:"policy"`
	}
	content, _ := os.ReadFile(policy["path"].(string))
	if err := json.Unmarshal(content, &file); err != nil {
		t.Fatal(err)
	}
	writes := 0
	for _, rule := range file.Policy.Tools.Allow {
		if rule["name"] == "local_write" {
			writes++
		}
	}
	if file.Policy.Level != "act-with-approval" || writes != 1 || len(file.Policy.Tools.Deny) != 0 {
		t.Errorf("policy file = %s", content)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the restart ran without an approval")
	}
	// Nothing else runs it either: a repeated run is refused, a report is
	// not there, and the task still waits.
	if code, _, errText := c.run("task", "run", taskID); code != exitError || !strings.Contains(errText, "is WaitingApproval") {
		t.Errorf("second run: %d %s", code, errText)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the restart ran on a refused run")
	}

	code, decided := c.json("task", "approve", taskID)
	if code != exitOK || decided["task"].(map[string]any)["status"] != "Completed" {
		t.Fatalf("approve: %d %v", code, decided)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("the restart did not run after the approval")
	}
	raw, _ = os.ReadFile(argsFile)
	args = strings.Join(strings.Split(strings.TrimSpace(string(raw)), "\n"), " ")
	if !strings.HasPrefix(args, "--session sess-1 --decide approve --json --approve none --call c1 --policy ") {
		t.Errorf("decide args = %s", args)
	}
	answer := decided["execution"].(map[string]any)["result"].(map[string]any)["answer"].(string)
	if code, out, _ := c.run("task", "report", taskID); code != exitOK || strings.TrimSpace(out) != answer {
		t.Errorf("report = %q", out)
	}

	// Denied instead: the session ends without the write.
	dir2 := t.TempDir()
	marker2 := filepath.Join(dir2, "restart-ran")
	bin2, _, _ := fakeaxlr.Write(dir2, fakeaxlr.Options{Scenario: "approval-flow", Marker: marker2})
	c.axlr = bin2
	_, submitted = c.json("task", "submit", "bot-act-with-approval", "--objective", "Create hello.txt again", "--key", "write-2")
	taskID = submitted["task"].(map[string]any)["id"].(string)
	if code, _ := c.json("task", "run", taskID); code != exitDecisionRequired {
		t.Fatal("second task did not wait")
	}
	if code, denied := c.json("task", "deny", taskID); code != exitOK || denied["task"].(map[string]any)["status"] != "Completed" {
		t.Errorf("deny: %d %v", code, denied)
	}
	if _, err := os.Stat(marker2); err == nil {
		t.Error("the restart ran after a denial")
	}
}

// The global --actor is who decides: a bot that lists approvers refuses
// the CLI's actor until it is one of them.
func TestTheActorDecides(t *testing.T) {
	dir := t.TempDir()
	bin, _, err := fakeaxlr.Write(dir, fakeaxlr.Options{Scenario: "approval-flow", Marker: filepath.Join(dir, "marker")})
	if err != nil {
		t.Fatal(err)
	}
	c := cli{t: t, state: t.TempDir(), axlr: bin}
	raw, _ := os.ReadFile(filepath.Join("testdata", "bot-act-with-approval.json"))
	var definition map[string]any
	_ = json.Unmarshal(raw, &definition)
	definition["approvals"] = map[string]any{"ttl": "15m", "approvers": []string{"tirso"}}
	file := filepath.Join(dir, "bot.json")
	encoded, _ := json.Marshal(definition)
	if err := os.WriteFile(file, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, bot := c.json("bot", "register", "--file", file); code != exitOK {
		t.Fatalf("register: %d %v", code, bot)
	}
	_, _ = c.json("bot", "enable", "bot-act-with-approval")
	_, submitted := c.json("task", "submit", "bot-act-with-approval", "--objective", "Restart demo-broken.service", "--key", "actor-1")
	taskID := submitted["task"].(map[string]any)["id"].(string)
	if code, _ := c.json("task", "run", taskID); code != exitDecisionRequired {
		t.Fatalf("run: %d", code)
	}
	if code, _, errText := c.run("task", "approve", taskID); code != exitError || !strings.Contains(errText, "test:cli is not among the approvers") {
		t.Errorf("approve as test:cli: %d %s", code, errText)
	}
	code, decided := c.json("--actor", "tirso", "task", "approve", taskID)
	if code != exitOK || decided["execution"].(map[string]any)["decision"] != "approve" {
		t.Errorf("approve as tirso: %d %v", code, decided)
	}
	for _, bad := range []string{`{"approvals":{"twoPersons":true}}`, `{"approvals":{"twoPersons":{"namespace":["prod"]}}}`, `{"approvals":{"twoPersons":{}}}`} {
		if code, _, errText := c.run("bot", "update", "bot-act-with-approval", "--file", writeJSON(t, dir, bad)); code == exitOK || !strings.Contains(errText, "twoPersons") && !strings.Contains(errText, "namespace") {
			t.Errorf("%s: %d %s", bad, code, errText)
		}
	}
	code, updated := c.json("bot", "update", "bot-act-with-approval", "--file", writeJSON(t, dir, `{"approvals":{"approvers":["tirso","oncall"],"twoPersons":{"namespaces":["prod"]}}}`))
	if approvals, _ := updated["approvals"].(map[string]any); code != exitOK || approvals["twoPersons"] == nil || len(approvals["approvers"].([]any)) != 2 {
		t.Errorf("update: %d %v", code, updated)
	}
}

func writeJSON(t *testing.T, dir, content string) string {
	t.Helper()
	file := filepath.Join(dir, "patch.json")
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}
