package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// TaskWaits says what each Pending task of a placed bot waits for while
// no node its placement admits may run it: what the task's page and the
// Tasks table show beside Pending, so a task that waits is never silent.
// It reads the nodes once and each Pending task's bot once.
type TaskWaits struct {
	deps Dependencies
}

// NewTaskWaits wires the use case.
func NewTaskWaits(deps Dependencies) TaskWaits { return TaskWaits{deps: deps} }

// Execute answers the waits of the tasks by task id; a task that waits
// for no node has none. A task whose bot is gone waits for nothing.
func (uc TaskWaits) Execute(ctx context.Context, tasks []domain.Task) (map[domain.TaskID]string, error) {
	waits := map[domain.TaskID]string{}
	var nodes []domain.Node
	read := false
	bots := map[domain.BotID]*domain.Bot{}
	for _, task := range tasks {
		if task.Status() != domain.TaskPending {
			continue
		}
		bot, seen := bots[task.BotID()]
		if !seen {
			found, err := uc.deps.Bots.Get(ctx, task.BotID())
			switch {
			case err == nil:
				bot = &found
			case !domain.IsNotFound(err):
				return nil, err
			}
			bots[task.BotID()] = bot
		}
		if bot == nil || bot.Placement().IsEmpty() {
			continue
		}
		if !read {
			listed, err := uc.deps.Nodes.List(ctx)
			if err != nil {
				return nil, err
			}
			nodes, read = listed, true
		}
		if waiting, ok := placementWait(*bot, nodes); ok {
			waits[task.ID()] = waiting
		}
	}
	return waits, nil
}
