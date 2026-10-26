package application

import (
	"context"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// RecordSaid takes what a node's running execution said back to the
// operator's message: a bot.task.said event in the task's timeline, so
// the operator reads the answer before the run ends. A node that does not
// hold the execution, or an execution that ended, is refused.
type RecordSaid struct {
	deps Dependencies
	rec  recorder
}

// NewRecordSaid wires the use case.
func NewRecordSaid(deps Dependencies) RecordSaid {
	return RecordSaid{deps: deps, rec: recorder{deps: deps}}
}

// Execute records the reply.
func (uc RecordSaid) Execute(ctx context.Context, node domain.NodeID, id domain.ExecutionID, reply SaidReply) error {
	words := strings.TrimSpace(reply.Text)
	if words == "" {
		return domain.ValidationError{Field: "text", Reason: "must not be empty"}
	}
	if runes := []rune(words); len(runes) > MaxSaidLength {
		words = string(runes[:MaxSaidLength-1]) + "…"
	}
	execution, err := uc.deps.Executions.Get(ctx, id)
	if err != nil {
		return err
	}
	if execution.NodeID() != node || execution.Outcome().IsFinal() {
		return domain.StateError{Subject: "execution " + id.String(), Current: "not running on node " + node.String(), Attempted: "say something"}
	}
	task, err := uc.deps.Tasks.Get(ctx, execution.TaskID())
	if err != nil {
		return err
	}
	bot, err := uc.deps.Bots.Get(ctx, task.BotID())
	if err != nil {
		return err
	}
	payload := map[string]any{"text": words}
	if reply.InReplyTo != "" {
		payload["inReplyTo"] = reply.InReplyTo
	}
	return uc.rec.executionEvent(ctx, domain.EventTaskSaid, bot, task.ID(), id, task.CorrelationID(), payload)
}
