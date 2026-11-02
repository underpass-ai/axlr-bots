package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/compose"
	"github.com/underpass-ai/axlr-bots/domain"
)

// catalogDir holds the ready-made definitions an operator registers as
// they are (catalog/README.md); its README says what each is for.
var catalogDir = filepath.Join("..", "..", "catalog")

// catalogPlaceholderSecret is the credential every webhook and
// alertmanager trigger of the catalog ships with: the README tells the
// operator to replace it, so a real one is never committed.
const catalogPlaceholderSecret = "replace-me-with-16-to-256-chars"

// catalogDefinitions reads every definition of the catalog as the command
// does, strictly, keyed by its file name without the extension.
func catalogDefinitions(t *testing.T) map[string]application.BotDefinition {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(catalogDir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("catalog: %v (%d files)", err, len(files))
	}
	definitions := make(map[string]application.BotDefinition, len(files))
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var definition application.BotDefinition
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&definition); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		definitions[strings.TrimSuffix(filepath.Base(file), ".json")] = definition
	}
	return definitions
}

// TestCatalogDefinitionsRegister registers every definition of the
// catalog into one control plane, through the use case and through the
// command, and checks what the README promises of them: the file is
// named after the bot, every bot a definition delegates to is in the
// catalog and takes its tasks, the credentials are still the placeholder,
// a Jev bot carries its program at the level it needs, and the README
// lists every definition and nothing else.
func TestCatalogDefinitionsRegister(t *testing.T) {
	definitions := catalogDefinitions(t)
	deps, err := compose.Dependencies(t.TempDir(), "/nonexistent", "test")
	if err != nil {
		t.Fatal(err)
	}
	bots := map[string]domain.Bot{}
	for name, definition := range definitions {
		bot, err := application.NewRegisterBot(deps).Execute(context.Background(), definition)
		if err != nil {
			t.Fatalf("%s.json: %v", name, err)
		}
		if bot.ID().String() != name || definition.ID != name || definition.Name != name {
			t.Errorf("%s.json registers %q (id %q, name %q): the file is named after the bot", name, bot.ID(), definition.ID, definition.Name)
		}
		bots[name] = bot
	}

	c := cli{t: t, state: t.TempDir(), axlr: fakeAXLR(t, "completed")}
	for name := range definitions {
		if code, _, errText := c.run("bot", "register", "--file", filepath.Join(catalogDir, name+".json")); code != exitOK {
			t.Errorf("bot register %s.json: %d %s", name, code, errText)
		}
	}

	for name, definition := range definitions {
		bot := bots[name]
		if definition.Delegation != nil {
			for _, target := range definition.Delegation.To {
				other, ok := bots[target]
				if !ok {
					t.Errorf("%s delegates to %s, which is not in the catalog", name, target)
					continue
				}
				if !other.AcceptsTrigger(domain.TriggerDelegation) && !other.IsTeammateOf(bot) {
					t.Errorf("%s delegates to %s, which takes no delegated task (no delegation trigger, no shared team)", name, target)
				}
			}
		}
		for _, trigger := range definition.Triggers {
			if credential := trigger.Secret + trigger.SigningKey; credential != "" && credential != catalogPlaceholderSecret {
				t.Errorf("%s trigger %s: the credential is not the placeholder the README says to replace", name, trigger.Name)
			}
		}
		if definition.Runtime != nil {
			if definition.Runtime.Kind != "jev" || definition.Level != "act-within-policy" || definition.Model.Model != "" || definition.Model.AllowRemoteProviders {
				t.Errorf("%s: a Jev bot runs its program within the policy and asks no language model: %+v %q %+v", name, definition.Runtime, definition.Level, definition.Model)
			}
			if _, ok := bot.Runtime().Jev(); !ok {
				t.Errorf("%s: the registered bot carries no Jev program", name)
			}
		}
		if definition.Budgets.TaskTimeoutSeconds == 0 || definition.Budgets.MaxToolCallsPerTask == 0 || definition.Budgets.MaxModelCallsPerTask == 0 {
			t.Errorf("%s: every budget is set: %+v", name, definition.Budgets)
		}
		if definition.Tenant != "default" || definition.Owner == "" {
			t.Errorf("%s: tenant %q owner %q", name, definition.Tenant, definition.Owner)
		}
	}

	readme, err := os.ReadFile(filepath.Join(catalogDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, match := range regexp.MustCompile(`\]\(([a-z0-9-]+)\.json\)`).FindAllStringSubmatch(string(readme), -1) {
		listed[match[1]] = true
	}
	var missing, stale []string
	for name := range definitions {
		if !listed[name] {
			missing = append(missing, name)
		}
	}
	for name := range listed {
		if _, ok := definitions[name]; !ok {
			stale = append(stale, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	if len(missing) > 0 || len(stale) > 0 {
		t.Errorf("README: not listed %v; listed without a file %v", missing, stale)
	}
}

// TestCatalogRefusesABrokenDefinition keeps the catalog test honest: a
// definition the control plane refuses is reported by its file.
func TestCatalogRefusesABrokenDefinition(t *testing.T) {
	deps, err := compose.Dependencies(t.TempDir(), "/nonexistent", "test")
	if err != nil {
		t.Fatal(err)
	}
	definitions := catalogDefinitions(t)
	for name, definition := range definitions {
		definition.ToolRules.Allow = append(definition.ToolRules.Allow, domain.ToolRuleSnapshot{Tool: "not_allowed_anywhere"})
		if _, err := application.NewRegisterBot(deps).Execute(context.Background(), definition); err == nil {
			t.Errorf("%s: a rule over a tool outside allowedTools registered", name)
		}
		break
	}
}
