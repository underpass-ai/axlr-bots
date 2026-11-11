package nodeagent

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// watcher serves one execution of a team's Jev watcher the tools of the
// reserved server axlr-bots, in process (docs/architecture/team-channel.md,
// A6): axlr_bots_watch_next puts the next open question of its team to
// Jev, or the next of its team's own tasks that has run 5 minutes
// (team-subscriptions.md, stuck work), axlr_bots_watch_decide hands Jev's
// choice for it to the control plane. It keeps only the question being decided and the round's cursor;
// the control plane decides what the watcher may see and touch.
type watcher struct {
	transport application.NodeTransport
	node      domain.NodeID
	execution domain.ExecutionID
	log       *log.Logger

	current domain.TaskID
	cursor  domain.TaskID
}

// call carries out one tool call of the run. A refusal of the control
// plane is the tool's error; a control plane that cannot be reached is the
// run's.
func (w *watcher) call(ctx context.Context, tool string, arguments map[string]any) (application.NodeToolAnswer, error) {
	switch domain.ToolName(tool) {
	case domain.WatchNextTool:
		return w.next(ctx)
	case domain.WatchDecideTool:
		return w.decide(ctx, arguments)
	}
	return application.NodeToolAnswer{Refused: "the node serves no tool " + tool + " on " + domain.WatchServer}, nil
}

// next asks the control plane for the next question of the round and
// puts it as Jev's ask; the cursor moves past it, whatever is decided, so
// a question comes once per round.
func (w *watcher) next(ctx context.Context) (application.NodeToolAnswer, error) {
	watched, err := w.transport.WatchNext(ctx, w.node, w.execution, w.cursor, true)
	if err != nil {
		return w.refused("watch", err)
	}
	if watched.Done {
		w.current = ""
		return application.NodeToolAnswer{Object: map[string]any{"done": true}}, nil
	}
	w.current, w.cursor = watched.Cursor, watched.Cursor
	if watched.Task != nil {
		// One of the team's own running tasks: stuck, or let it run
		// (docs/architecture/team-subscriptions.md, stuck work).
		ask := map[string]any{
			"state":    watched.Task.Text(),
			"question": fmt.Sprintf("Is this task of team %s stuck, or should it keep running?", watched.Team),
			"options":  []any{domain.WatchWait, domain.WatchStuck},
		}
		return application.NodeToolAnswer{Object: map[string]any{"ask": ask, "task": watched.Cursor.String()}}, nil
	}
	ask := map[string]any{
		"state":    watched.View.Text(),
		"question": fmt.Sprintf("Should team %s keep waiting for this answer or abandon the question?", watched.Team),
		"options":  []any{domain.WatchWait, domain.WatchAbandon},
	}
	return application.NodeToolAnswer{Object: map[string]any{"ask": ask, "question": watched.Cursor.String()}}, nil
}

// decide hands Jev's choice and verdict for the current question over.
func (w *watcher) decide(ctx context.Context, arguments map[string]any) (application.NodeToolAnswer, error) {
	if w.current == "" {
		return application.NodeToolAnswer{Refused: "no question is being decided: call " + domain.WatchNextTool.String() + " first"}, nil
	}
	choice, _ := arguments[domain.WatchChoice].(string)
	verdict, _ := arguments[domain.WatchVerdict].(map[string]any)
	probability, _ := verdict["probability"].(float64)
	confidence, _ := verdict["confidence"].(float64)
	decision := application.WatchDecision{Question: w.current, Choice: choice, Probability: probability, Confidence: confidence}
	decided, err := w.transport.DecideWatch(ctx, w.node, w.execution, decision)
	if err != nil {
		return w.refused("decide on question "+w.current.String(), err)
	}
	w.current = ""
	if decided.Abandoned || decided.Stuck {
		w.log.Printf("node: execution %s: question %s %s", w.execution, decided.Question, decided.Reason)
	}
	object := map[string]any{"question": decided.Question.String(), "choice": decided.Choice, "status": decided.Status.String(), "abandoned": decided.Abandoned}
	if decided.Stuck {
		object["stuck"] = true
	}
	if decided.Reason != "" {
		object["reason"] = decided.Reason
	}
	return application.NodeToolAnswer{Object: object}, nil
}

// refused is a refusal of the control plane as the tool's error, and any
// other failure as the run's.
func (w *watcher) refused(what string, err error) (application.NodeToolAnswer, error) {
	var (
		forbidden  domain.ForbiddenError
		notFound   domain.NotFoundError
		state      domain.StateError
		validation domain.ValidationError
	)
	if errors.As(err, &forbidden) || errors.As(err, &notFound) || errors.As(err, &state) || errors.As(err, &validation) {
		w.log.Printf("node: execution %s: %s refused: %v", w.execution, what, err)
		return application.NodeToolAnswer{Refused: err.Error()}, nil
	}
	return application.NodeToolAnswer{}, fmt.Errorf("%s: %w", what, err)
}
