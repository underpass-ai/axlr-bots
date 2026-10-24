package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/underpass-ai/underpass-bots/application"
	"github.com/underpass-ai/underpass-bots/domain"
)

// TestExampleBotsDenyByDefault reads the two example bots as the command
// does and checks their rules are tight: a file-reading command may name
// one path under /var/log (a `*` cannot cross a `/`, so a second absolute
// path never matches), nothing starts with `**` in front of a path, the
// places that hold keys are denied, and local writes are denied.
func TestExampleBotsDenyByDefault(t *testing.T) {
	for _, name := range []string{"sre-host-bot.json", "sre-host-fixer.json"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "examples", name))
		if err != nil {
			t.Fatal(err)
		}
		var definition application.BotDefinition
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&definition); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		tools, err := domain.NewAllowedTools(definition.AllowedTools)
		if err != nil {
			t.Fatal(err)
		}
		rules, err := domain.RestoreToolRules(definition.ToolRules, tools)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		denied := map[string]bool{}
		for _, rule := range rules.Deny() {
			denied[rule.Tool().String()+" "+rule.Arguments()["args"]] = true
		}
		for _, want := range []string{"local_write ", "local_edit ", "local_exec **/.ssh/**", "local_exec **/.config/**", "local_exec **underpass-bots/env**"} {
			if !denied[want] {
				t.Errorf("%s: deny list lacks %q", name, want)
			}
		}
		for _, rule := range rules.Allow() {
			program, pattern := rule.Arguments()["program"], rule.Arguments()["args"]
			switch program {
			case "tail", "head", "ls", "cat", "grep", "find":
				if pattern == "" || strings.HasPrefix(pattern, "**") || !strings.Contains(pattern, "/var/log") {
					t.Errorf("%s: %s rule %q is not anchored under /var/log", name, program, pattern)
				}
				if strings.Contains(strings.TrimSuffix(pattern, "]"), "**") {
					t.Errorf("%s: %s rule %q lets a second path in", name, program, pattern)
				}
			case "systemctl":
				if !strings.HasPrefix(pattern, `["`) {
					t.Errorf("%s: systemctl rule %q is not anchored to the first argument", name, pattern)
				}
				if name == "sre-host-bot.json" && strings.Contains(pattern, "restart") {
					t.Errorf("%s: the observe bot may restart: %q", name, pattern)
				}
			case "", "journalctl", "df", "free", "uptime", "ss", "ps", "dmesg":
			default:
				t.Errorf("%s: unexpected program %q", name, program)
			}
		}
		if definition.Level == "observe" && rules.Constrains("local_read") {
			t.Errorf("%s: local_read should be allowed as a whole", name)
		}
	}
}
