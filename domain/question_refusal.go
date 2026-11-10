package domain

// Reasons a question is refused that no delegation or event shares; a
// question is also refused as RefusedNoTeam, RefusedNotAllowed,
// RefusedDepthExceeded and RefusedRateLimited. The control plane checks
// them in the order of the team channel's ADR: no_team, own_team,
// not_allowed, depth_exceeded, rate_limited, team_busy, team_unavailable.
const (
	// RefusedOwnTeam: the bot asked a team it belongs to; inside a team,
	// it delegates.
	RefusedOwnTeam = "own_team"
	// RefusedTeamBusy: the asked team already has as many questions
	// waiting as it may queue.
	RefusedTeamBusy = "team_busy"
	// RefusedTeamUnavailable: no member of the asked team may answer
	// now: none consents to the asker's team, is enabled and has an
	// online node that shares the team's memory.
	RefusedTeamUnavailable = "team_unavailable"
)

// QuestionRefusal is a question the control plane did not send: which
// check refused it and why, in words the model can act on.
type QuestionRefusal struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}
