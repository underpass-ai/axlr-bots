package domain

import (
	"testing"
	"time"
)

// canal is the team channel's about in every team's store: no bot takes
// it, neither registered nor restored.
func TestTheChannelsIDIsReserved(t *testing.T) {
	if !ChannelBotID.Reserved() || BotID("canal-2").Reserved() || BotID("dev-go").Reserved() {
		t.Fatal("only canal is reserved")
	}
	if id, err := NewBotID("canal"); err != nil || id != ChannelBotID {
		t.Fatalf("the id still parses, so history that names it reads: %q %v", id, err)
	}
	if _, err := NewBot(ChannelBotID, "default", "canal", "operator", "You answer.", AllowedTools{}, ModelPolicy{}, Budgets{}, time.Unix(1, 0)); err == nil {
		t.Error("registered a bot named canal")
	}
	snapshot := storedBotSnapshot()
	snapshot.ID = "canal"
	if _, err := RestoreBot(snapshot); err == nil {
		t.Error("restored a bot named canal")
	}
}
