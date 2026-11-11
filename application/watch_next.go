package application

import (
	"context"
	"slices"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// WatchAfter is how old a question is, with no answer, before its team's
// Jev watcher is put it (docs/architecture/team-channel.md, A6): a younger
// question is never offered. The watcher runs on a timer of its own.
const WatchAfter = 5 * time.Minute

// WatchNext is a team's Jev watcher asking for the next question its
// round decides on: the first open question of its team, WatchAfter old or
// more, after the cursor, in a stable order (oldest first, then by id).
// None left is Done. Each question comes once per round: the cursor moves
// past it whatever was decided. A watcher whose node asks for tasks is
// also put its team's own tasks that have run WatchAfter or more, in the
// same order (docs/architecture/team-subscriptions.md, stuck work); an
// older node asks for none.
type WatchNext struct {
	deps Dependencies
}

// NewWatchNext wires the use case.
func NewWatchNext(deps Dependencies) WatchNext { return WatchNext{deps: deps} }

// Execute answers the next question after the cursor; an empty cursor
// starts the round.
func (uc WatchNext) Execute(ctx context.Context, node domain.NodeID, executionID domain.ExecutionID, after domain.TaskID, tasks bool) (WatchedQuestion, error) {
	w, err := watcherOf(ctx, uc.deps, node, executionID, "watch questions")
	if err != nil {
		return WatchedQuestion{}, err
	}
	open, err := w.open(ctx)
	if err != nil {
		return WatchedQuestion{}, err
	}
	if tasks {
		running, err := w.running(ctx)
		if err != nil {
			return WatchedQuestion{}, err
		}
		open = append(open, running...)
		slices.SortFunc(open, watchOrder)
	}
	if after != "" {
		cursor, err := uc.deps.Tasks.Get(ctx, after)
		if err != nil {
			return WatchedQuestion{}, err
		}
		next := len(open)
		for i, task := range open {
			if watchOrder(task, cursor) > 0 {
				next = i
				break
			}
		}
		open = open[next:]
	}
	if len(open) == 0 {
		return WatchedQuestion{Done: true, Team: w.team}, nil
	}
	if open[0].Trigger().Type != domain.TriggerQuestion {
		since, _, err := w.runningSince(ctx, open[0])
		if err != nil {
			return WatchedQuestion{}, err
		}
		view, err := readTaskWatchView(ctx, uc.deps, w.team, open[0], since)
		if err != nil {
			return WatchedQuestion{}, err
		}
		return WatchedQuestion{Team: w.team, Task: &view, Cursor: open[0].ID()}, nil
	}
	view, err := readQuestionView(ctx, uc.deps, open[0])
	if err != nil {
		return WatchedQuestion{}, err
	}
	return WatchedQuestion{Team: w.team, View: view, Cursor: open[0].ID()}, nil
}
