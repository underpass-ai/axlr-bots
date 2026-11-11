package application

import (
	"context"

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
