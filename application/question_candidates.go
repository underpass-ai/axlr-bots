package application

import (
	"context"
	"slices"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// questionRoster is what the checks and the pick read under the runner's
// lock: the tenant's bots, the nodes, the open tasks of every bot, and
// the open questions of every team and asker.
type questionRoster struct {
	bots  []domain.Bot
	nodes []domain.Node
	open  map[domain.BotID]int
	// queued counts the Pending questions of each answering team.
	queued map[domain.TeamName]int
	// asked counts the unanswered questions of each asking bot.
	asked map[domain.BotID]int
}

// readQuestionRoster reads the roster once.
func readQuestionRoster(ctx context.Context, deps Dependencies, tenant domain.TenantID) (questionRoster, error) {
	all, err := deps.Bots.List(ctx)
	if err != nil {
		return questionRoster{}, err
	}
	nodes, err := deps.Nodes.List(ctx)
	if err != nil {
		return questionRoster{}, err
	}
	r := questionRoster{nodes: nodes, open: map[domain.BotID]int{}, queued: map[domain.TeamName]int{}, asked: map[domain.BotID]int{}}
	byID := map[domain.BotID]domain.Bot{}
	for _, bot := range all {
		if bot.TenantID() == tenant {
			r.bots = append(r.bots, bot)
			byID[bot.ID()] = bot
		}
	}
	slices.SortFunc(r.bots, func(a, b domain.Bot) int { return strings.Compare(a.ID().String(), b.ID().String()) })
	for _, status := range []domain.TaskStatus{domain.TaskPending, domain.TaskRunning, domain.TaskWaitingApproval} {
		tasks, err := deps.Tasks.ListByStatus(ctx, status)
		if err != nil {
			return questionRoster{}, err
		}
		for _, task := range tasks {
			bot, ok := byID[task.BotID()]
			if !ok {
				continue
			}
			r.open[bot.ID()]++
			if task.Trigger().Type != domain.TriggerQuestion {
				continue
			}
			if status == domain.TaskPending {
				r.queued[memoryTeamOf(bot)]++
			}
			if lineage, ok := task.Lineage(); ok {
				r.asked[domain.BotID(lineage.ParentBotID)]++
			}
		}
	}
	return r, nil
}

// candidates are the members of the team that may answer the asking
// team now (docs/architecture/team-channel.md, "Who is free"): the team
// is their memory team, they consent to the asking team, they take tasks,
// and an Online node may run them and shares the team's memory. By id.
func (r questionRoster) candidates(team, asking domain.TeamName) []domain.Bot {
	var out []domain.Bot
	for _, bot := range r.bots {
		if memoryTeamOf(bot) != team || !bot.AnswersQuestionsFrom(asking) || !bot.AcceptsTasks() {
			continue
		}
		if slices.ContainsFunc(r.nodes, func(node domain.Node) bool { return answersOn(bot, node) }) {
			out = append(out, bot)
		}
	}
	return out
}

// pick chooses one candidate: the first free one by id, free being no
// task Pending, Running or waiting for a person; with none free, the one
// with the fewest open tasks, then by id. False when there is none.
func (r questionRoster) pick(candidates []domain.Bot) (domain.Bot, bool) {
	if len(candidates) == 0 {
		return domain.Bot{}, false
	}
	best := candidates[0]
	for _, bot := range candidates[1:] {
		if r.open[bot.ID()] < r.open[best.ID()] {
			best = bot
		}
	}
	return best, true
}

// answersOn reports whether the node may run the bot's questions: it may
// run the bot, and it opens the store of the bot's memory team. A node
// older than the team channel names no memory team and never answers.
func answersOn(bot domain.Bot, node domain.Node) bool {
	return nodeCanRun(bot, domain.SkillPins{}, node) && node.Capabilities().SharesMemoryOf(memoryTeamOf(bot))
}
