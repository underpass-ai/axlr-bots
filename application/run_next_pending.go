package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// RunNextPending runs the oldest Pending task once, through RunTask, in
// the control plane's own process: the single-process mode. A task whose
// bot an Online node may run is left to that node (NextAssignment). The
// daemon's worker calls it on every tick, so a task submitted by another
// process (over the socket or straight into the state directory while
// the daemon was down) is picked up without a hand-off message.
type RunNextPending struct {
	deps Dependencies
}

// NewRunNextPending wires the use case.
func NewRunNextPending(deps Dependencies) RunNextPending {
	return RunNextPending{deps: deps}
}

// Execute runs the oldest Pending task and returns its result with ran
// true; with no Pending task it returns ran false and nothing else.
func (uc RunNextPending) Execute(ctx context.Context) (RunTaskResult, bool, error) {
	pending, err := uc.deps.Tasks.ListByStatus(ctx, domain.TaskPending)
	if err != nil {
		return RunTaskResult{}, false, err
	}
	nodes, err := uc.deps.Nodes.List(ctx)
	if err != nil {
		return RunTaskResult{}, false, err
	}
	var oldest *domain.Task
	for i := range pending {
		bot, err := uc.deps.Bots.Get(ctx, pending[i].BotID())
		if err != nil {
			return RunTaskResult{}, false, err
		}
		if hasOnlineNode(bot, nodes) {
			continue
		}
		if oldest == nil || pending[i].CreatedAt().Before(oldest.CreatedAt()) {
			oldest = &pending[i]
		}
	}
	if oldest == nil {
		return RunTaskResult{}, false, nil
	}
	result, err := NewRunTask(uc.deps).Execute(ctx, oldest.ID())
	if err != nil {
		return RunTaskResult{}, true, err
	}
	return result, true, nil
}

// hasOnlineNode reports a bot an Online node may run.
func hasOnlineNode(bot domain.Bot, nodes []domain.Node) bool {
	for _, node := range nodes {
		if node.IsOnline() && bot.Placement().Admits(node) {
			return true
		}
	}
	return false
}
