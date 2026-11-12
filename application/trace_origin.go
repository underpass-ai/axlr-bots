package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// traceOrigin is the origin of the other team's domain event the task's
// chain began with: its own, or the first one found going up its parents
// (a teammate's event or a delegation makes a child with a lineage and no
// origin). Nil when the chain began inside the team. So a crossed task's
// teammates count the trace's crossings too, and the limit holds along
// the whole chain (docs/architecture/team-topics.md, decision 6). A
// parent that is gone ends the walk.
func traceOrigin(ctx context.Context, deps Dependencies, task domain.Task) (*domain.EventOrigin, error) {
	for range domain.MaxDelegationDepthLimit + 1 {
		if origin, ok := task.Origin(); ok {
			return &origin, nil
		}
		lineage, ok := task.Lineage()
		if !ok {
			return nil, nil
		}
		parent, err := deps.Tasks.Get(ctx, domain.TaskID(lineage.ParentTaskID))
		if domain.IsNotFound(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		task = parent
	}
	return nil, nil
}
