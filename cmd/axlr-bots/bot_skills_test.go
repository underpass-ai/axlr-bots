package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/internal/fakeaxlr"
)

func skilledCLI(t *testing.T, options fakeaxlr.Options) (cli, string) {
	t.Helper()
	dir := t.TempDir()
	options.Skills = true
	bin, args, err := fakeaxlr.Write(dir, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	return cli{t: t, state: t.TempDir(), axlr: bin}, args
}

func TestABotDefinedWithSkillsRunsThemPinned(t *testing.T) {
	c, argsFile := skilledCLI(t, fakeaxlr.Options{Scenario: "completed"})
	code, out, errText := c.run("bot", "register", "--file", filepath.Join("testdata", "bot-skills.json"))
	if code != exitOK || !strings.Contains(out, `"skills"`) || !strings.Contains(out, `"release_notes"`) {
		t.Fatalf("register: %d %s %s", code, out, errText)
	}
	taskID := submitted(t, c)
	if got := taskText(t, c, taskID); !strings.Contains(got, `"release_notes"`) {
		t.Fatalf("the task does not carry the pin: %s", got)
	}
	if code, out, errText := c.run("task", "run", taskID); code != exitOK {
		t.Fatalf("task run: %d %s %s", code, out, errText)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil || !strings.Contains(string(args), "--skills\n") {
		t.Fatalf("args = %s %v", args, err)
	}
	var skillsFile string
	lines := strings.Split(string(args), "\n")
	for i, line := range lines {
		if line == "--skills" {
			skillsFile = lines[i+1]
		}
	}
	raw, err := os.ReadFile(skillsFile)
	if err != nil || !strings.Contains(string(raw), `"release_notes"`) {
		t.Errorf("skills file = %s %v", raw, err)
	}
	if !strings.Contains(string(args), "axlr_skill") && !strings.Contains(readPolicy(t, c.state), "axlr_skill") {
		t.Error("the policy leaves the skill unreadable")
	}
}

func TestATaskFailsWhenItsSkillCannotBeInstalled(t *testing.T) {
	c, argsFile := skilledCLI(t, fakeaxlr.Options{Scenario: "completed", SkillInstallError: "axlr-tui skills install: catalog axlr has no skill release_notes 1.0.0"})
	if code, _ := c.json("bot", "register", "--file", filepath.Join("testdata", "bot-skills.json")); code != exitOK {
		t.Fatal(code)
	}
	taskID := submitted(t, c)
	code, out, _ := c.run("task", "run", taskID)
	if code == exitOK || !strings.Contains(out, "catalog skill axlr/release_notes@1.0.0 could not be installed") || !strings.Contains(out, "has no skill release_notes 1.0.0") {
		t.Errorf("task run: %d %s", code, out)
	}
	if _, err := os.Stat(argsFile); !os.IsNotExist(err) {
		t.Error("the run started without its skill")
	}
}

func TestRegisterRefusesARangeForASkill(t *testing.T) {
	c, _ := skilledCLI(t, fakeaxlr.Options{Scenario: "completed"})
	raw, err := os.ReadFile(filepath.Join("testdata", "bot-skills.json"))
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "bot.json")
	if err := os.WriteFile(file, []byte(strings.Replace(string(raw), `"version": "1.0.0"`, `"version": "latest"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errText := c.run("bot", "register", "--file", file)
	if code == exitOK || !strings.Contains(errText, "exact major.minor.patch") {
		t.Errorf("register: %d %s", code, errText)
	}
}

// submitted enables the bot and submits one task, returning its id.
func submitted(t *testing.T, c cli) string {
	t.Helper()
	if code, _ := c.json("bot", "enable", "bot-skills"); code != exitOK {
		t.Fatalf("enable: %d", code)
	}
	code, answer := c.json("task", "submit", "bot-skills", "--objective", "List the pods", "--key", "k-1")
	task, ok := answer["task"].(map[string]any)
	if code != exitOK || !ok {
		t.Fatalf("submit: %d %v", code, answer)
	}
	return task["id"].(string)
}

func taskText(t *testing.T, c cli, id string) string {
	t.Helper()
	code, out, errText := c.run("task", "get", id)
	if code != exitOK {
		t.Fatalf("get: %d %s", code, errText)
	}
	return out
}

func readPolicy(t *testing.T, state string) string {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(state, "policies", "*.json"))
	if len(matches) == 0 {
		t.Fatal("no policy file")
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
