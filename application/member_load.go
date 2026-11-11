package application

import (
	"context"
	"slices"

	"github.com/underpass-ai/axlr-bots/domain"
)

// memberLoad counts the open tasks of every bot, open being Pending,
// Running or waiting for a person: what the pick of a team member reads
// (docs/architecture/team-subscriptions.md, decision 7).
type memberLoad struct {
	open map[domain.BotID]int
}

// openStatuses are the statuses a task is open in.
var openStatuses = []domain.TaskStatus{domain.TaskPending, domain.TaskRunning, domain.TaskWaitingApproval}

// readMemberLoad reads the open tasks of every bot once; each counts
// when keep says so (nil keeps all).
func readMemberLoad(ctx context.Context, deps Dependencies, keep func(domain.Task, domain.TaskStatus) bool) (memberLoad, error) {
	load := memberLoad{open: map[domain.BotID]int{}}
	for _, status := range openStatuses {
		tasks, err := deps.Tasks.ListByStatus(ctx, status)
		if err != nil {
			return memberLoad{}, err
		}
		for _, task := range tasks {
			if keep == nil || keep(task, status) {
				load.open[task.BotID()]++
			}
		}
	}
	return load, nil
}

// pick chooses one candidate, given by id: the first free one, free
// being no open task; with none free, the one with the fewest open
// tasks, then by id. False when there is none.
func (l memberLoad) pick(candidates []domain.Bot) (domain.Bot, bool) {
	if len(candidates) == 0 {
		return domain.Bot{}, false
	}
	best := candidates[0]
	for _, bot := range candidates[1:] {
		if l.open[bot.ID()] < l.open[best.ID()] {
			best = bot
		}
	}
	return best, true
}

// pickMember chooses one of the candidates, given by id, for a task of
// its team: among those some Online node may run (all of them when none
// is, so the task waits Pending for a node as a task does), the first
// free one, else the least busy. The candidates are not empty.
func pickMember(ctx context.Context, deps Dependencies, candidates []domain.Bot) (domain.Bot, error) {
	nodes, err := deps.Nodes.List(ctx)
	if err != nil {
		return domain.Bot{}, err
	}
	var pool []domain.Bot
	for _, bot := range candidates {
		if slices.ContainsFunc(nodes, func(node domain.Node) bool { return nodeCanRun(bot, domain.SkillPins{}, node) }) {
			pool = append(pool, bot)
		}
	}
	if len(pool) == 0 {
		pool = candidates
	}
	load, err := readMemberLoad(ctx, deps, nil)
	if err != nil {
		return domain.Bot{}, err
	}
	picked, _ := load.pick(pool)
	return picked, nil
}
