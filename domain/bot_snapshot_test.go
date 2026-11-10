package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

// A bot stored before the team channel, as the store writes it, reads and
// writes back byte for byte: no ask, no from, nothing new.
func TestABotWithoutTheTeamChannelIsStoredAsItAlwaysWas(t *testing.T) {
	snapshot := storedBotSnapshot()
	snapshot.Groups = []string{"qa"}
	snapshot.Delegation = &DelegationSnapshot{To: []string{"dev-go"}}
	snapshot.Triggers = []TriggerSnapshot{{Type: "manual", Name: "manual"}, {Type: "delegation", Name: "delegation"}, {Type: "timer", Name: "daily", Every: "24h0m0s", Objective: "Look again"}}
	snapshot.Approvals = ApprovalsSnapshot{TTL: "1h", OnExpire: "deny"}
	before, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	bot, err := RestoreBot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(bot.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("before: %s\nafter:  %s", before, after)
	}
	for _, field := range []string{`"ask"`, `"from"`} {
		if strings.Contains(string(after), field) {
			t.Errorf("%s in %s", field, after)
		}
	}
	if bot.Ask().Enabled() || bot.AnswersQuestionsFrom("dev") {
		t.Error("a bot without the fields neither asks nor answers")
	}
}

// A bot with both sides of the channel keeps them through the store.
func TestABotKeepsItsAskAndQuestionTrigger(t *testing.T) {
	snapshot := storedBotSnapshot()
	snapshot.Groups = []string{"dev"}
	snapshot.Ask = &AskSnapshot{Teams: []string{"qa", "producto"}, PerMinute: 2, MaxOpen: 3}
	snapshot.Triggers = []TriggerSnapshot{{Type: "manual", Name: "manual"}, {Type: "question", Name: "question", From: []string{"qa", "producto"}}}
	snapshot.Approvals = ApprovalsSnapshot{TTL: "1h", OnExpire: "deny"}
	before, _ := json.Marshal(snapshot)
	bot, err := RestoreBot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(bot.Snapshot())
	if string(before) != string(after) {
		t.Errorf("before: %s\nafter:  %s", before, after)
	}
	if !bot.Ask().Allows("producto") || !bot.AnswersQuestionsFrom("qa") {
		t.Errorf("ask %+v, answers qa %v", bot.Ask(), bot.AnswersQuestionsFrom("qa"))
	}
}
