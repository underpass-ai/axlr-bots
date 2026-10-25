package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/compose"
	"github.com/underpass-ai/axlr-bots/domain"
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

// TestSidecarExampleBot registers the sidecar's example bot as the
// control plane would and checks it matches the chart's demo: observe,
// AXLR's read tools only, local_read limited to log files, placed on the
// demo's app label, its id the workspace the chart mounts the log at.
func TestSidecarExampleBot(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "sidecar", "bot-log-summariser.json"))
	if err != nil {
		t.Fatal(err)
	}
	var definition application.BotDefinition
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&definition); err != nil {
		t.Fatal(err)
	}
	deps, err := compose.Dependencies(t.TempDir(), "/nonexistent", "test")
	if err != nil {
		t.Fatal(err)
	}
	bot, err := application.NewRegisterBot(deps).Execute(context.Background(), definition)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	snapshot := bot.Snapshot()
	if snapshot.Level != "observe" || len(snapshot.ToolRules.Allow) != 1 || snapshot.ToolRules.Allow[0].Tool != "local_read" || snapshot.ToolRules.Allow[0].Arguments["path"] != "**/*.log" {
		t.Errorf("level %q rules %+v", snapshot.Level, snapshot.ToolRules)
	}
	for _, tool := range snapshot.AllowedTools {
		if tool != "local_list" && tool != "local_read" && tool != "local_search" {
			t.Errorf("tool %q is not a read", tool)
		}
	}
	values, err := os.ReadFile(filepath.Join("..", "..", "charts", "axlr-bots-node", "values.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// The demo released as "demo" is demo-log-service; its pods carry app=demo-log-service.
	if snapshot.Placement.Selector["app"] != "demo-log-service" || !strings.Contains(string(values), "\n  name: log-service\n") || !strings.Contains(string(values), "\n  bot: "+snapshot.ID+"\n") {
		t.Errorf("placement %+v does not match the chart's demo", snapshot.Placement)
	}
}

// TestDelegationExampleBotsFormAPair registers the delegation examples
// as the command does: bot-a may ask bot-b, which takes delegated tasks
// and asks nobody.
func TestDelegationExampleBotsFormAPair(t *testing.T) {
	deps, err := compose.Dependencies(t.TempDir(), "/nonexistent", "test")
	if err != nil {
		t.Fatal(err)
	}
	bots := map[string]domain.Bot{}
	for _, name := range []string{"bot-delegating.json", "bot-delegate-target.json"} {
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
		bot, err := application.NewRegisterBot(deps).Execute(context.Background(), definition)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		bots[bot.ID().String()] = bot
	}
	a, b := bots["bot-a"], bots["bot-b"]
	if !a.Delegation().Allows("bot-b") || a.DelegationDepth() != 1 || a.Level() != domain.AutonomyObserve {
		t.Errorf("bot-a = %+v", a.Snapshot().Delegation)
	}
	if b.Delegation().Enabled() || !b.AcceptsTrigger(domain.TriggerDelegation) {
		t.Errorf("bot-b = %+v triggers %+v", b.Snapshot().Delegation, b.Snapshot().Triggers)
	}
}
