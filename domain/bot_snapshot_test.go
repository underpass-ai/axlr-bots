package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

// A bot stored as the store writes it reads and
// writes back byte for byte: nothing new.
func TestABotIsStoredAsItAlwaysWas(t *testing.T) {
	snapshot := storedBotSnapshot()
	snapshot.Groups = []string{"ops"}
	snapshot.Delegation = &DelegationSnapshot{To: []string{"worker-1"}}
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
}
