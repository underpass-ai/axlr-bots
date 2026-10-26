package application

import (
	"context"
	"slices"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// teammatesOf lists, sorted by id, every other bot of the bot's tenant
// that shares a team with it, whatever its status.
func teammatesOf(ctx context.Context, deps Dependencies, bot domain.Bot) ([]domain.Bot, error) {
	if bot.Teams().Empty() {
		return nil, nil
	}
	all, err := deps.Bots.List(ctx)
	if err != nil {
		return nil, err
	}
	var mates []domain.Bot
	for _, other := range all {
		if bot.IsTeammateOf(other) {
			mates = append(mates, other)
		}
	}
	slices.SortFunc(mates, func(a, b domain.Bot) int { return strings.Compare(a.ID().String(), b.ID().String()) })
	return mates, nil
}

// directoryOf is what the bot reads of its teams before a task: its
// Enabled teammates, the first 32 by id.
func directoryOf(bot domain.Bot, mates []domain.Bot) domain.TeamDirectory {
	var members []domain.Teammate
	for _, mate := range mates {
		if mate.AcceptsTasks() && len(members) < domain.MaxDirectoryMembers {
			members = append(members, domain.TeammateOf(bot, mate))
		}
	}
	return domain.NewTeamDirectory(bot.Teams().Names(), members)
}

// delegationTargets are the bots the bot may ask to do a task: those its
// delegation names, and its Enabled teammates that take its tasks,
// sorted and without repeats.
func delegationTargets(bot domain.Bot, mates []domain.Bot) []string {
	var targets []string
	for _, id := range bot.Delegation().To() {
		targets = append(targets, id.String())
	}
	for _, mate := range mates {
		if mate.AcceptsTasks() && mate.TakesDelegationFrom(bot) {
			targets = append(targets, mate.ID().String())
		}
	}
	slices.Sort(targets)
	return slices.Compact(targets)
}
