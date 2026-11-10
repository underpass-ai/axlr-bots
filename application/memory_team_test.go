package application

import "testing"

// A bot's run opens its team's memory: the assignment names the team, the
// first by name for a bot in several, and none for a bot in no team,
// whose memory stays its own.
func TestAssignmentNamesTheTeamTheBotSharesItsMemoryWith(t *testing.T) {
	h := newHarness()
	onlineNode(t, h, "web-1")
	for id, tc := range map[string]struct {
		groups []string
		want   string
	}{
		"qa-jefe":      {[]string{"qa"}, "qa"},
		"mkt-analista": {[]string{"mkt-escucha", "mkt-direccion"}, "mkt-direccion"},
		"alone":        {nil, ""},
	} {
		bot := teamBot(t, h, id, tc.groups, nil)
		if run := runOnNode(t, h, bot, "web-1", "ask-"+id).Run; run == nil || run.MemoryTeam.String() != tc.want {
			t.Errorf("%s shares its memory with %q, want %q", id, run.MemoryTeam, tc.want)
		}
	}
}
