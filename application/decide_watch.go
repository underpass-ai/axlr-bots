package application

import (
	"context"
	"fmt"

	"github.com/underpass-ai/axlr-bots/domain"
)

// DecideWatch carries out what a team's Jev watcher decided for one of
// its team's questions: wait changes nothing; abandon cancels the
// question's task, with Jev's verdict as the reason, and the return leg
// (answerDeliverer) brings the asker answered false with that reason. No
// event of its own: the cancel and bot.question.answered are the record,
// and the audit is question.abandon.
type DecideWatch struct {
	deps Dependencies
}

// NewDecideWatch wires the use case.
func NewDecideWatch(deps Dependencies) DecideWatch { return DecideWatch{deps: deps} }

// Execute decides. A question that is not the watcher's team's is not
// found; one that ended meanwhile is left as it is; one younger than
// WatchAfter is refused.
func (uc DecideWatch) Execute(ctx context.Context, node domain.NodeID, executionID domain.ExecutionID, decision WatchDecision) (WatchDecided, error) {
	w, err := watcherOf(ctx, uc.deps, node, executionID, "decide on a question")
	if err != nil {
		return WatchDecided{}, err
	}
	if err := decision.validate(); err != nil {
		return WatchDecided{}, err
	}
	task, err := uc.deps.Tasks.Get(ctx, decision.Question)
	if err != nil {
		return WatchDecided{}, err
	}
	ours, err := w.belongs(ctx, task)
	if err != nil {
		return WatchDecided{}, err
	}
	if !ours {
		if work, err := w.belongsAsTask(ctx, task); err != nil {
			return WatchDecided{}, err
		} else if work {
			return uc.decideTask(ctx, w, task, decision)
		}
		// Another team's question, or no question: not the watcher's to see.
		return WatchDecided{}, domain.NotFoundError{Kind: "question", ID: decision.Question.String()}
	}
	decided := WatchDecided{Question: task.ID(), Choice: decision.Choice, Status: task.Status()}
	if task.Status().IsTerminal() {
		return decided, nil
	}
	if !w.due(task) {
		return WatchDecided{}, domain.StateError{Subject: "question " + task.ID().String(), Current: "younger than " + WatchAfter.String(), Attempted: "decide on it"}
	}
	if decision.Choice == domain.WatchStuck {
		return WatchDecided{}, domain.ValidationError{Field: "choice", Reason: "a question is waited for or abandoned, never " + domain.WatchStuck}
	}
	if decision.Choice == domain.WatchWait {
		return decided, nil
	}
	responder, err := uc.deps.Bots.Get(ctx, task.BotID())
	if err != nil {
		return WatchDecided{}, err
	}
	reason := fmt.Sprintf("abandoned by %s: Jev chose %s (p=%.2f, c=%.2f)", w.bot.ID(), domain.WatchAbandon, decision.Probability, decision.Confidence)
	extra := map[string]any{"watcher": w.bot.ID().String(), "choice": decision.Choice, "probability": decision.Probability, "confidence": decision.Confidence}
	result, err := stopTask(ctx, uc.deps, responder, task, reason, domain.EventTaskCancelled, "question.abandon", extra)
	if err != nil {
		return WatchDecided{}, err
	}
	decided.Status, decided.Abandoned, decided.Reason = result.Task.Status(), true, reason
	return decided, nil
}

// decideTask carries out the watcher's decision for one of its team's own
// running tasks (docs/architecture/team-subscriptions.md, stuck work):
// wait changes nothing; stuck cancels the task with Jev's verdict as the
// reason and tells the bot's teammates warn.stuck, so the one of them a
// team subscription picks takes the work over. A task that ended or is
// no longer running is left as it is; one that has not run WatchAfter is
// refused.
func (uc DecideWatch) decideTask(ctx context.Context, w questionWatcher, task domain.Task, decision WatchDecision) (WatchDecided, error) {
	if decision.Choice == domain.WatchAbandon {
		return WatchDecided{}, domain.ValidationError{Field: "choice", Reason: "a team task is let run or judged " + domain.WatchStuck + ", never " + domain.WatchAbandon}
	}
	decided := WatchDecided{Question: task.ID(), Choice: decision.Choice, Status: task.Status()}
	since, running, err := w.runningSince(ctx, task)
	if err != nil {
		return WatchDecided{}, err
	}
	if !running {
		return decided, nil
	}
	if uc.deps.Clock.Now().Sub(since) < WatchAfter {
		return WatchDecided{}, domain.StateError{Subject: "task " + task.ID().String(), Current: "running for less than " + WatchAfter.String(), Attempted: "judge it"}
	}
	if decision.Choice == domain.WatchWait {
		return decided, nil
	}
	bot, err := uc.deps.Bots.Get(ctx, task.BotID())
	if err != nil {
		return WatchDecided{}, err
	}
	execution := task.CurrentExecutionID()
	reason := fmt.Sprintf("stuck, judged by %s: Jev chose %s (p=%.2f, c=%.2f)", w.bot.ID(), domain.WatchStuck, decision.Probability, decision.Confidence)
	extra := map[string]any{"watcher": w.bot.ID().String(), "choice": decision.Choice, "probability": decision.Probability, "confidence": decision.Confidence}
	result, err := stopTask(ctx, uc.deps, bot, task, reason, domain.EventTaskCancelled, "task.stuck", extra)
	if err != nil {
		return WatchDecided{}, err
	}
	if err := newPlatformPublisher(uc.deps).taskStuck(ctx, bot, result.Task, execution, reason); err != nil {
		return WatchDecided{}, err
	}
	decided.Status, decided.Stuck, decided.Reason = result.Task.Status(), true, reason
	return decided, nil
}
