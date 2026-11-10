package domain

import (
	"encoding/json"
	"testing"
)

func TestAskPolicyValidatesItsTeamsAndLimits(t *testing.T) {
	ask, err := NewAskPolicy(AskSnapshot{Teams: []string{"dev", "producto"}, PerMinute: 5, MaxOpen: 8})
	if err != nil || !ask.Enabled() || !ask.Allows("producto") || ask.Allows("qa") || ask.PerMinute() != 5 || ask.MaxOpen() != 8 {
		t.Fatalf("ask = %+v %v", ask, err)
	}
	if teams := ask.Teams(); len(teams) != 2 || teams[0] != "dev" || teams[1] != "producto" {
		t.Errorf("teams keep the definition's order: %v", teams)
	}
	if s := ask.Snapshot(); s == nil || len(s.Teams) != 2 || s.PerMinute != 5 || s.MaxOpen != 8 {
		t.Errorf("snapshot = %+v", s)
	}
	var none AskPolicy
	if none.Enabled() || none.Snapshot() != nil || none.PerMinute() != DefaultAskPerMinute || none.MaxOpen() != DefaultAskMaxOpen || none.Allows("dev") {
		t.Errorf("zero value = %+v", none)
	}
	// Limits named alone are kept, so the definition reads back as given,
	// and the bot is still offered no tool.
	if alone, err := NewAskPolicy(AskSnapshot{PerMinute: 1}); err != nil || alone.Enabled() || alone.Snapshot() == nil || alone.Snapshot().PerMinute != 1 {
		t.Errorf("perMinute alone = %+v %v", alone.Snapshot(), err)
	}
	nine := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}
	for _, bad := range []AskSnapshot{
		{Teams: nine}, {Teams: []string{"Dev"}}, {Teams: []string{"dev", "dev"}}, {Teams: []string{""}},
		{PerMinute: -1}, {PerMinute: 11}, {MaxOpen: -1}, {MaxOpen: 9},
	} {
		if _, err := NewAskPolicy(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestAskSnapshotReadsTheADRsJSON(t *testing.T) {
	var s AskSnapshot
	if err := json.Unmarshal([]byte(`{"teams":["dev"],"perMinute":2,"maxOpen":3}`), &s); err != nil {
		t.Fatal(err)
	}
	ask, err := NewAskPolicy(s)
	if err != nil || !ask.Allows("dev") || ask.PerMinute() != 2 || ask.MaxOpen() != 3 {
		t.Fatalf("ask = %+v %v", ask, err)
	}
	out, _ := json.Marshal(ask.Snapshot())
	if string(out) != `{"teams":["dev"],"perMinute":2,"maxOpen":3}` {
		t.Errorf("written: %s", out)
	}
}

// A bot asks other teams only: one of its own is refused, on a definition
// and on a stored bot alike, and a later change of its teams is checked.
func TestBotAsksNoneOfItsOwnTeams(t *testing.T) {
	bot := teamTestBot(t, "qa-jefe", "default", []string{"qa"})
	own, _ := NewAskPolicy(AskSnapshot{Teams: []string{"dev", "qa"}})
	bot.SetAsk(own)
	if err := bot.CheckAsk(); err == nil {
		t.Fatal("a bot asking its own team")
	}
	other, _ := NewAskPolicy(AskSnapshot{Teams: []string{"dev"}})
	bot.SetAsk(other)
	if err := bot.CheckAsk(); err != nil || !bot.Ask().Allows("dev") {
		t.Fatalf("ask = %+v %v", bot.Ask(), err)
	}
	moved, _ := NewTeams([]string{"dev"})
	bot.JoinTeams(moved)
	if err := bot.CheckAsk(); err == nil {
		t.Fatal("a bot that joined the team it asks")
	}

	snapshot := storedBotSnapshot()
	snapshot.Groups = []string{"qa"}
	snapshot.Ask = &AskSnapshot{Teams: []string{"dev"}, MaxOpen: 2}
	restored, err := RestoreBot(snapshot)
	if err != nil || !restored.Ask().Allows("dev") || restored.Ask().MaxOpen() != 2 {
		t.Fatalf("restored: %v %+v", err, restored.Ask())
	}
	if got := restored.Snapshot().Ask; got == nil || got.Teams[0] != "dev" || got.MaxOpen != 2 {
		t.Errorf("snapshot = %+v", got)
	}
	snapshot.Ask = &AskSnapshot{Teams: []string{"qa"}}
	if _, err := RestoreBot(snapshot); err == nil {
		t.Error("restored a bot asking its own team")
	}
	snapshot.Ask = &AskSnapshot{PerMinute: 40}
	if _, err := RestoreBot(snapshot); err == nil {
		t.Error("restored a bad ask")
	}
}
