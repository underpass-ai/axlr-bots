package application

import (
	"cmp"
	"context"
	"slices"

	"github.com/underpass-ai/axlr-bots/domain"
)

// The bounds of a task list: what a list answers when it names no
// limit, and the most it answers.
const (
	DefaultTaskLimit = 100
	MaxTaskLimit     = 1000
)

// ListTasks answers the tasks that match a query, across bots, newest
// first: what an operator scans to see what the fleet is doing.
type ListTasks struct {
	deps Dependencies
}

// NewListTasks wires the use case.
func NewListTasks(deps Dependencies) ListTasks { return ListTasks{deps: deps} }

// Execute validates the query and answers at most its limit of tasks,
// the newest created first. A bot that does not exist is not found.
func (uc ListTasks) Execute(ctx context.Context, query TaskQuery) ([]domain.Task, error) {
	var bot domain.BotID
	if query.BotID != "" {
		id, err := domain.NewBotID(query.BotID)
		if err != nil {
			return nil, err
		}
		if _, err := uc.deps.Bots.Get(ctx, id); err != nil {
			return nil, err
		}
		bot = id
	}
	var status domain.TaskStatus
	if query.Status != "" {
		parsed, err := domain.ParseTaskStatus(query.Status)
		if err != nil {
			return nil, err
		}
		status = parsed
	}
	if query.Limit < 0 {
		return nil, domain.ValidationError{Field: "limit", Reason: "must not be negative"}
	}
	limit := query.Limit
	switch {
	case limit == 0:
		limit = DefaultTaskLimit
	case limit > MaxTaskLimit:
		limit = MaxTaskLimit
	}
	all, err := uc.deps.Tasks.List(ctx)
	if err != nil {
		return nil, err
	}
	tasks := make([]domain.Task, 0, len(all))
	for _, task := range all {
		if bot != "" && task.BotID() != bot {
			continue
		}
		if status != "" && task.Status() != status {
			continue
		}
		if task.CreatedAt().Before(query.Since) {
			continue
		}
		tasks = append(tasks, task)
	}
	slices.SortFunc(tasks, func(a, b domain.Task) int {
		if c := b.CreatedAt().Compare(a.CreatedAt()); c != 0 {
			return c
		}
		return cmp.Compare(b.ID().String(), a.ID().String())
	})
	if len(tasks) > limit {
		tasks = tasks[:limit]
	}
	return tasks, nil
}
