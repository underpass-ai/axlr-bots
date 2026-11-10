package domain

import (
	"encoding/json"
	"testing"
)

// A question's refusals are the ADR's seven, each a distinct word, and the
// four it shares with delegations and events keep their values.
func TestQuestionRefusalsAreTheADRsSeven(t *testing.T) {
	reasons := []string{RefusedNoTeam, RefusedOwnTeam, RefusedNotAllowed, RefusedDepthExceeded, RefusedRateLimited, RefusedTeamBusy, RefusedTeamUnavailable}
	want := []string{"no_team", "own_team", "not_allowed", "depth_exceeded", "rate_limited", "team_busy", "team_unavailable"}
	seen := map[string]bool{}
	for i, reason := range reasons {
		if reason != want[i] || seen[reason] {
			t.Errorf("reason %d = %q", i, reason)
		}
		seen[reason] = true
	}
	out, _ := json.Marshal(QuestionRefusal{Reason: RefusedTeamBusy, Message: "dev has 8 questions waiting"})
	if string(out) != `{"reason":"team_busy","message":"dev has 8 questions waiting"}` {
		t.Errorf("written: %s", out)
	}
}
