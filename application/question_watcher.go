package application

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"github.com/underpass-ai/axlr-bots/domain"
)

// questionWatcher is a team's Jev watcher at work: the bot whose
// execution a node holds, and the team whose questions it watches
// (docs/architecture/team-channel.md, A6). The control plane decides who
// watches what: a watcher sees and touches only the questions its own
// team asked.
type questionWatcher struct {
	deps Dependencies
	bot  domain.Bot
	team domain.TeamName
}

// watcherOf finds the watcher behind a node's execution. A node with the
// team channel off, a bot that is not a Jev watcher, or one in no team,
// is refused.
func watcherOf(ctx context.Context, deps Dependencies, node domain.NodeID, executionID domain.ExecutionID, attempted string) (questionWatcher, error) {
	execution, err := runningExecutionOf(ctx, deps, node, executionID, attempted)
	if err != nil {
		return questionWatcher{}, err
	}
	bot, err := deps.Bots.Get(ctx, execution.BotID())
	if err != nil {
		return questionWatcher{}, err
	}
	w := questionWatcher{deps: deps, bot: bot, team: memoryTeamOf(bot)}
	if _, err := channelNodeOf(ctx, deps, node, attempted); err != nil {
		var forbidden domain.ForbiddenError
		if errors.As(err, &forbidden) {
			return questionWatcher{}, w.refuse(ctx, attempted, forbidden.Reason)
		}
		return questionWatcher{}, err
	}
	switch {
	case !bot.WatchesQuestions():
		return questionWatcher{}, w.refuse(ctx, attempted, "bot "+bot.ID().String()+" is not a Jev watcher: only a Jev bot whose program names the server "+domain.WatchServer+" watches its team's questions")
	case w.team == "":
		return questionWatcher{}, w.refuse(ctx, attempted, "bot "+bot.ID().String()+" is in no team: it has no questions to watch")
	}
	return w, nil
}

// refuse audits a refused watch and answers why.
func (w questionWatcher) refuse(ctx context.Context, attempted, reason string) error {
	_ = recorder{deps: w.deps}.audit(ctx, "question.watch", w.bot.ID().String(), domain.AuditDenied, map[string]string{"attempted": attempted, "reason": reason})
	return domain.ForbiddenError{Reason: reason}
}

// belongs reports whether the task is a question the watcher's team
// asked: its trigger names the team and its asker's memory team is it.
// An asker that is gone belongs to no team.
func (w questionWatcher) belongs(ctx context.Context, task domain.Task) (bool, error) {
	if task.Trigger().Type != domain.TriggerQuestion || task.Trigger().Name != w.team.String() {
		return false, nil
	}
	lineage, ok := task.Lineage()
	if !ok {
		return false, nil
	}
	asker, err := w.deps.Bots.Get(ctx, domain.BotID(lineage.ParentBotID))
	if domain.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return memoryTeamOf(asker) == w.team, nil
}

// due reports whether a question is put to Jev now: open, and asked
// WatchAfter ago or more.
func (w questionWatcher) due(task domain.Task) bool {
	return !task.Status().IsTerminal() && w.deps.Clock.Now().Sub(task.CreatedAt()) >= WatchAfter
}

// open lists the team's questions that are due, oldest first, then by id.
func (w questionWatcher) open(ctx context.Context) ([]domain.Task, error) {
	var questions []domain.Task
	for _, status := range []domain.TaskStatus{domain.TaskPending, domain.TaskRunning, domain.TaskWaitingApproval} {
		tasks, err := w.deps.Tasks.ListByStatus(ctx, status)
		if err != nil {
			return nil, err
		}
		for _, task := range tasks {
			if !w.due(task) {
				continue
			}
			ours, err := w.belongs(ctx, task)
			if err != nil {
				return nil, err
			}
			if ours {
				questions = append(questions, task)
			}
		}
	}
	slices.SortFunc(questions, watchOrder)
	return questions, nil
}

// watchOrder is the round's stable order: oldest first, then by id.
func watchOrder(a, b domain.Task) int {
	if c := a.CreatedAt().Compare(b.CreatedAt()); c != 0 {
		return c
	}
	return cmp.Compare(a.ID(), b.ID())
}
