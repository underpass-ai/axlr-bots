package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewSkillPinsTakesExactVersions(t *testing.T) {
	pins, err := NewSkillPins([]SkillPinSnapshot{{Name: "release_notes", Version: "1.2.3", Catalog: "axlr"}, {Name: "release_notes", Version: "2.0.0", Catalog: "lab-1"}})
	if err != nil {
		t.Fatal(err)
	}
	all := pins.All()
	if len(all) != 2 || all[0].Key() != "axlr/release_notes@1.2.3" || all[1].Name() != "release_notes" || all[1].Version() != "2.0.0" || all[1].Catalog() != "lab-1" || pins.IsEmpty() {
		t.Errorf("pins = %+v", all)
	}
	if got := pins.Snapshot(); len(got) != 2 || got[0] != (SkillPinSnapshot{Name: "release_notes", Version: "1.2.3", Catalog: "axlr"}) {
		t.Errorf("snapshot = %+v", got)
	}
	none, err := NewSkillPins(nil)
	if err != nil || !none.IsEmpty() || none.Snapshot() != nil || len(none.All()) != 0 {
		t.Errorf("none = %+v %v", none, err)
	}
}

func TestNewSkillPinRefuses(t *testing.T) {
	good := SkillPinSnapshot{Name: "oom", Version: "1.0.0", Catalog: "axlr"}
	for name, change := range map[string]func(*SkillPinSnapshot){
		"latest":      func(s *SkillPinSnapshot) { s.Version = "latest" },
		"caret":       func(s *SkillPinSnapshot) { s.Version = "^1.0.0" },
		"tilde":       func(s *SkillPinSnapshot) { s.Version = "~1.0.0" },
		"wildcard":    func(s *SkillPinSnapshot) { s.Version = "1.x" },
		"star":        func(s *SkillPinSnapshot) { s.Version = "*" },
		"short":       func(s *SkillPinSnapshot) { s.Version = "1.0" },
		"prefix":      func(s *SkillPinSnapshot) { s.Version = "v1.0.0" },
		"prerelease":  func(s *SkillPinSnapshot) { s.Version = "1.0.0-rc.1" },
		"leading 0":   func(s *SkillPinSnapshot) { s.Version = "01.0.0" },
		"no version":  func(s *SkillPinSnapshot) { s.Version = "" },
		"no name":     func(s *SkillPinSnapshot) { s.Name = "" },
		"bad name":    func(s *SkillPinSnapshot) { s.Name = "Oom" },
		"flag name":   func(s *SkillPinSnapshot) { s.Name = "--yes" },
		"path name":   func(s *SkillPinSnapshot) { s.Name = "a/../b" },
		"no catalog":  func(s *SkillPinSnapshot) { s.Catalog = "" },
		"bad catalog": func(s *SkillPinSnapshot) { s.Catalog = "../other" },
	} {
		s := good
		change(&s)
		var invalidError ValidationError
		if _, err := NewSkillPin(s); !errors.As(err, &invalidError) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestNewSkillPinsRefusesRepeatsAndTooMany(t *testing.T) {
	one := SkillPinSnapshot{Name: "oom", Version: "1.0.0", Catalog: "axlr"}
	other := SkillPinSnapshot{Name: "oom", Version: "1.1.0", Catalog: "axlr"}
	if _, err := NewSkillPins([]SkillPinSnapshot{one, other}); err == nil || !strings.Contains(err.Error(), "axlr/oom is pinned twice") {
		t.Errorf("repeat: %v", err)
	}
	many := make([]SkillPinSnapshot, MaxSkillPins+1)
	for i := range many {
		many[i] = SkillPinSnapshot{Name: "s" + strings.Repeat("a", i+1), Version: "1.0.0", Catalog: "axlr"}
	}
	if _, err := NewSkillPins(many); err == nil || !strings.Contains(err.Error(), "at most 32") {
		t.Errorf("too many: %v", err)
	}
	if _, err := NewSkillPins(many[:MaxSkillPins]); err != nil {
		t.Errorf("exactly the limit: %v", err)
	}
}

func storedBotSnapshot() BotSnapshot {
	at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	return BotSnapshot{
		ID: "bot", TenantID: "t", Name: "bot", Owner: "o", Mission: "m", AllowedTools: []string{"local_read"},
		Model: ModelPolicySnapshot{Model: "m", RoutingPolicy: "cost-optimized", MaxContextTokens: 32000}, Budgets: BudgetsSnapshot{MaxModelCallsPerTask: 1, MaxToolCallsPerTask: 1, TaskTimeoutSeconds: 60},
		Level: "observe", Triggers: []TriggerSnapshot{{Type: "manual"}}, Status: "Registered", CreatedAt: at, UpdatedAt: at,
	}
}

// A bot stored before the field reads and writes back without it.
func TestABotWithoutSkillsIsStoredAsItAlwaysWas(t *testing.T) {
	bot, err := RestoreBot(storedBotSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if !bot.Skills().IsEmpty() {
		t.Errorf("skills = %+v", bot.Skills())
	}
	raw, err := json.Marshal(bot.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "skills") {
		t.Errorf("stored: %s", raw)
	}
}

func TestABotKeepsItsSkillsAndAJevProgramTakesNone(t *testing.T) {
	snapshot := storedBotSnapshot()
	snapshot.Skills = []SkillPinSnapshot{{Name: "oom", Version: "1.0.0", Catalog: "axlr"}}
	bot, err := RestoreBot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if got := bot.Snapshot().Skills; len(got) != 1 || got[0].Name != "oom" {
		t.Errorf("skills = %+v", got)
	}
	snapshot.Skills[0].Version = "latest"
	if _, err := RestoreBot(snapshot); err == nil {
		t.Error("a stored bot pinned to latest was read")
	}
	jev := storedBotSnapshot()
	jev.AllowedTools, jev.Level, jev.Runtime = []string{"batches", "process", "listen", "stop"}, "act-within-policy", jevSnapshot()
	if _, err := RestoreBot(jev); err != nil {
		t.Fatal(err)
	}
	jev.Skills = []SkillPinSnapshot{{Name: "oom", Version: "1.0.0", Catalog: "axlr"}}
	if _, err := RestoreBot(jev); err == nil || !strings.Contains(err.Error(), "no language model") {
		t.Errorf("jev with skills: %v", err)
	}
}

func TestATaskKeepsTheSkillsItWasSubmittedWith(t *testing.T) {
	task, err := NewTask("task-1", "bot", "o", "k", "c", time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := json.Marshal(task.Snapshot()); strings.Contains(string(raw), "skills") {
		t.Errorf("a task without skills is stored with them: %s", raw)
	}
	pins, _ := NewSkillPins([]SkillPinSnapshot{{Name: "oom", Version: "1.0.0", Catalog: "axlr"}})
	task.PinSkills(pins)
	back, err := RestoreTask(task.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Skills().All(); len(got) != 1 || got[0].Key() != "axlr/oom@1.0.0" {
		t.Errorf("restored = %+v", got)
	}
	bad := task.Snapshot()
	bad.Skills[0].Version = "^1"
	if _, err := RestoreTask(bad); err == nil {
		t.Error("a stored task pinned to a range was read")
	}
}
