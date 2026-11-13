package application

import (
	"context"
	"errors"

	"github.com/underpass-ai/axlr-bots/domain"
)

// GetTaskActivity builds a task's activity tree: the task, the tasks it
// delegated (recursively, by its children), each execution with its
// usage and cost, the task's whole timeline and, where an execution's
// progress timeline was cut, the cut and the content completed from the
// transcript (docs/architecture/task-activity.md).
type GetTaskActivity struct {
	deps Dependencies
}

// NewGetTaskActivity wires the use case.
func NewGetTaskActivity(deps Dependencies) GetTaskActivity { return GetTaskActivity{deps: deps} }

// Execute answers the tree under the task as it is now. A task that does
// not exist is not found; a child that vanished is left out.
func (uc GetTaskActivity) Execute(ctx context.Context, id domain.TaskID, options ActivityOptions) (TaskActivity, error) {
	task, err := uc.deps.Tasks.Get(ctx, id)
	if err != nil {
		return TaskActivity{}, err
	}
	b := activityBuilder{deps: uc.deps, options: options, visited: map[domain.TaskID]bool{}}
	return b.build(ctx, task, options.depth())
}

// activityBuilder walks the tree once; visited guards against a lineage
// that loops.
type activityBuilder struct {
	deps    Dependencies
	options ActivityOptions
	visited map[domain.TaskID]bool
}

func (b *activityBuilder) build(ctx context.Context, task domain.Task, depth int) (TaskActivity, error) {
	b.visited[task.ID()] = true
	events, err := allTaskEvents(ctx, b.deps.EventReader, task.ID())
	if err != nil {
		return TaskActivity{}, err
	}
	out := TaskActivity{Task: task, Events: events}
	for _, execID := range task.ExecutionIDs() {
		execution, err := b.deps.Executions.Get(ctx, execID)
		if err != nil {
			var missing domain.NotFoundError
			if errors.As(err, &missing) {
				continue
			}
			return TaskActivity{}, err
		}
		activity, err := b.execution(ctx, execution, events)
		if err != nil {
			return TaskActivity{}, err
		}
		out.Executions = append(out.Executions, activity)
		out.SubtreeUsage = addUsage(out.SubtreeUsage, execution.Usage())
	}
	if depth <= 0 {
		return out, nil
	}
	for _, childID := range task.Children() {
		if b.visited[childID] {
			continue
		}
		child, err := b.deps.Tasks.Get(ctx, childID)
		if err != nil {
			var missing domain.NotFoundError
			if errors.As(err, &missing) {
				continue
			}
			return TaskActivity{}, err
		}
		sub, err := b.build(ctx, child, depth-1)
		if err != nil {
			return TaskActivity{}, err
		}
		out.Children = append(out.Children, sub)
		out.SubtreeUsage = addUsage(out.SubtreeUsage, sub.SubtreeUsage)
	}
	return out, nil
}

// addUsage sums two usages; a cost unknown on either side stays unknown
// and the known costs still add up.
func addUsage(a, b domain.Usage) domain.Usage {
	return domain.Usage{
		Requests:         a.Requests + b.Requests,
		PromptTokens:     a.PromptTokens + b.PromptTokens,
		CachedTokens:     a.CachedTokens + b.CachedTokens,
		CompletionTokens: a.CompletionTokens + b.CompletionTokens,
		ReasoningTokens:  a.ReasoningTokens + b.ReasoningTokens,
		ToolCalls:        a.ToolCalls + b.ToolCalls,
		CostUSD:          a.CostUSD + b.CostUSD,
		CostUnknown:      a.CostUnknown || b.CostUnknown,
	}
}

// allTaskEvents reads the task's whole timeline: the port answers the
// newest limit events, so it asks for more until it gets fewer than it
// asked.
func allTaskEvents(ctx context.Context, reader EventReader, task domain.TaskID) ([]domain.Event, error) {
	limit := MaxEventLimit
	for {
		events, err := reader.ListTaskEvents(ctx, task, limit)
		if err != nil {
			return nil, err
		}
		if len(events) < limit || limit >= MaxActivityEvents {
			if events == nil {
				events = []domain.Event{}
			}
			return events, nil
		}
		limit = min(limit*4, MaxActivityEvents)
	}
}
