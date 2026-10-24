package application

import (
	"context"

	"github.com/underpass-ai/underpass-bots/domain"
)

// RunNextPending runs the oldest Pending task once, through RunTask. The
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
	if len(pending) == 0 {
		return RunTaskResult{}, false, nil
	}
	oldest := pending[0]
	for _, task := range pending[1:] {
		if task.CreatedAt().Before(oldest.CreatedAt()) {
			oldest = task
		}
	}
	result, err := NewRunTask(uc.deps).Execute(ctx, oldest.ID())
	if err != nil {
		return RunTaskResult{}, true, err
	}
	return result, true, nil
}
