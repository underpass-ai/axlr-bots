package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNewStagedDefinitionWantsAVersionATimeAndAnObject(t *testing.T) {
	at := time.Date(2026, 11, 12, 10, 0, 0, 0, time.FixedZone("CET", 3600))
	staged, err := NewStagedDefinition("1.2.0", at, []byte(` {"mission": "m", "version": "1.2.0"} `))
	if err != nil {
		t.Fatal(err)
	}
	if staged.Version() != "1.2.0" || !staged.StagedAt().Equal(at) || staged.StagedAt().Location() != time.UTC || string(staged.Change()) != `{"mission": "m", "version": "1.2.0"}` {
		t.Fatalf("staged = %+v", staged)
	}
	change := staged.Change()
	change[0] = 'X'
	if staged.Change()[0] != '{' {
		t.Fatal("Change hands out a copy")
	}
	cases := map[string]func() error{
		"no version":  func() error { _, err := NewStagedDefinition("", at, []byte(`{}`)); return err },
		"bad version": func() error { _, err := NewStagedDefinition("v1", at, []byte(`{}`)); return err },
		"no time":     func() error { _, err := NewStagedDefinition("1.0.0", time.Time{}, []byte(`{}`)); return err },
		"no change":   func() error { _, err := NewStagedDefinition("1.0.0", at, nil); return err },
		"an array":    func() error { _, err := NewStagedDefinition("1.0.0", at, []byte(`[1]`)); return err },
		"broken json": func() error { _, err := NewStagedDefinition("1.0.0", at, []byte(`{"a":`)); return err },
		"over one MiB": func() error {
			_, err := NewStagedDefinition("1.0.0", at, []byte(`{"m":"`+strings.Repeat("a", 1<<20)+`"}`))
			return err
		},
		"restore no ver": func() error {
			_, err := RestoreStagedDefinition(StagedDefinitionSnapshot{StagedAt: at, Change: []byte(`{}`)})
			return err
		},
	}
	for name, try := range cases {
		if try() == nil {
			t.Errorf("%s was taken", name)
		}
	}
	back, err := RestoreStagedDefinition(staged.Snapshot())
	if err != nil || back.Version() != staged.Version() || !back.StagedAt().Equal(staged.StagedAt()) || string(back.Change()) != string(staged.Change()) {
		t.Fatalf("restored %+v %v", back, err)
	}
}

// A bot with a version and a staged change keeps both through the store;
// an answer about it shows the staged version and time, never the change.
func TestABotKeepsItsVersionAndStagedChange(t *testing.T) {
	bot, err := RestoreBot(storedBotSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if !bot.Version().IsZero() {
		t.Fatalf("a bot stored before versions names one: %q", bot.Version())
	}
	if _, ok := bot.Staged(); ok || bot.Unstage() {
		t.Fatal("a bot stored before staging holds nothing staged")
	}
	at := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	staged, err := NewStagedDefinition("1.1.0", at, []byte(`{"version":"1.1.0","triggers":[{"type":"webhook","name":"hook","secret":"a-secret-of-sixteen-plus"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	bot.SetVersion("1.0.0")
	bot.Stage(staged)
	stored := bot.Snapshot()
	raw, _ := json.Marshal(stored)
	if !strings.Contains(string(raw), `"version":"1.0.0"`) || !strings.Contains(string(raw), `"staged":{"version":"1.1.0","stagedAt":"2026-11-12T09:00:00Z","change":{`) {
		t.Fatalf("stored: %s", raw)
	}
	back, err := RestoreBot(stored)
	if err != nil {
		t.Fatal(err)
	}
	held, ok := back.Staged()
	if back.Version() != "1.0.0" || !ok || held.Version() != "1.1.0" || string(held.Change()) != string(staged.Change()) {
		t.Fatalf("restored %q %+v %v", back.Version(), held, ok)
	}
	answered, _ := json.Marshal(stored.Redacted())
	if strings.Contains(string(answered), "change") || strings.Contains(string(answered), "a-secret") || !strings.Contains(string(answered), `"staged":{"version":"1.1.0","stagedAt":"2026-11-12T09:00:00Z"}`) {
		t.Fatalf("answered: %s", answered)
	}
	if stored.Staged.Change == nil {
		t.Fatal("Redacted changed the stored snapshot")
	}
	if !back.Unstage() {
		t.Fatal("the staged change was there")
	}
	if _, ok := back.Staged(); ok || back.Snapshot().Staged != nil {
		t.Fatal("unstaged, nothing is held")
	}
	back.SetVersion("")
	if raw, _ := json.Marshal(back.Snapshot()); strings.Contains(string(raw), `"version"`) || strings.Contains(string(raw), "staged") {
		t.Fatalf("no version, nothing staged: %s", raw)
	}
}

func TestRestoreBotRefusesABadVersionOrStagedChange(t *testing.T) {
	snapshot := storedBotSnapshot()
	snapshot.Version = "v1.0.0"
	if _, err := RestoreBot(snapshot); err == nil {
		t.Error("a bad version was taken")
	}
	snapshot = storedBotSnapshot()
	snapshot.Staged = &StagedDefinitionSnapshot{Version: "1.0.0", StagedAt: time.Now()}
	if _, err := RestoreBot(snapshot); err == nil {
		t.Error("a staged change without its patch (a redacted answer) was taken")
	}
}
