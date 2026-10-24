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

// TestExampleBotsAreRoleNeutral reads the example definitions as the
// command does and checks they carry no role: a neutral mission, AXLR's
// own tools only, manual triggers, placement on any node; the observe
// bot allows no write, the act-with-approval bot allows local_write only.
func TestExampleBotsAreRoleNeutral(t *testing.T) {
	for name, level := range map[string]string{"bot.json": "observe", "bot-act-with-approval.json": "act-with-approval"} {
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
		if definition.Level != level || !strings.Contains(strings.ToLower(definition.Mission), "within the policy") {
			t.Errorf("%s: level %q mission %q", name, definition.Level, definition.Mission)
		}
		tools, err := domain.NewAllowedTools(definition.AllowedTools)
		if err != nil {
			t.Fatal(err)
		}
		for _, tool := range tools.Names() {
			if !strings.HasPrefix(tool, "local_") {
				t.Errorf("%s: tool %q is not one of AXLR's own", name, tool)
			}
			if level == "observe" && (tool == "local_write" || tool == "local_edit" || tool == "local_exec") {
				t.Errorf("%s: observe bot allows %q", name, tool)
			}
		}
		if level == "act-with-approval" && (!tools.Allows("local_write") || tools.Allows("local_exec")) {
			t.Errorf("%s: tools = %v", name, tools.Names())
		}
		if len(definition.Triggers) != 1 || definition.Triggers[0].Type != "manual" || len(definition.Placement.Nodes) != 0 || len(definition.Placement.Selector) != 0 {
			t.Errorf("%s: triggers %+v placement %+v", name, definition.Triggers, definition.Placement)
		}
		if definition.Budgets.TaskTimeoutSeconds == 0 || definition.Budgets.MaxToolCallsPerTask == 0 {
			t.Errorf("%s: budgets %+v", name, definition.Budgets)
		}
	}
}
