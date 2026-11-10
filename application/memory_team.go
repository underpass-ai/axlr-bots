package application

import "github.com/underpass-ai/axlr-bots/domain"

// memoryTeamOf is the team a bot shares its memory with
// (docs/architecture/memory-store-per-team.md): the first of its teams
// as the control plane keeps them, which is sorted by name, and none for
// a bot in no team, whose memory stays its own. A bot in one team, as
// every bot of the QA and developer teams is, has no order to mind.
func memoryTeamOf(bot domain.Bot) domain.TeamName {
	names := bot.Teams().Names()
	if len(names) == 0 {
		return ""
	}
	return names[0]
}
