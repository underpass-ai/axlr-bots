package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// SendTaskMessage takes what the operator writes to a task while it runs.
// The message becomes a bot.task.message event naming the execution it
// goes to; the node running that execution reads it with its lease and
// hands it to the run between model steps. Only a Running task with an
// execution on a node takes a message: any other task is refused, and
// the operator sends it a new task instead.
type SendTaskMessage struct {
	deps Dependencies
	rec  recorder
}

// NewSendTaskMessage wires the use case.
func NewSendTaskMessage(deps Dependencies) SendTaskMessage {
	return SendTaskMessage{deps: deps, rec: recorder{deps: deps}}
}

// Execute records the message for the task's running execution.
func (uc SendTaskMessage) Execute(ctx context.Context, id domain.TaskID, raw string) (SentTaskMessage, error) {
	text, err := domain.NewOperatorMessage(raw)
	if err != nil {
		return SentTaskMessage{}, err
	}
	task, err := uc.deps.Tasks.Get(ctx, id)
	if err != nil {
		return SentTaskMessage{}, err
	}
	refuse := func(current string) (SentTaskMessage, error) {
		refused := domain.StateError{Subject: "task " + id.String(), Current: current, Attempted: "take a message (send it a new task)"}
		_ = uc.rec.audit(ctx, "task.message", id.String(), domain.AuditDenied, map[string]string{"reason": refused.Error()})
		return SentTaskMessage{}, refused
	}
	if task.Status() != domain.TaskRunning || task.CurrentExecutionID() == "" {
		return refuse(task.Status().String())
	}
	execution, err := uc.deps.Executions.Get(ctx, task.CurrentExecutionID())
	if err != nil {
		return SentTaskMessage{}, err
	}
	if execution.Outcome().IsFinal() || execution.NodeID() == "" {
		return refuse("running on no node")
	}
	bot, err := uc.deps.Bots.Get(ctx, task.BotID())
	if err != nil {
		return SentTaskMessage{}, err
	}
	sent := SentTaskMessage{TaskID: id, ExecutionID: execution.ID(), CreatedAt: uc.deps.Clock.Now()}
	if err := uc.rec.executionEvent(ctx, domain.EventTaskMessage, bot, id, execution.ID(), task.CorrelationID(), map[string]any{"text": text.String(), "from": uc.deps.Actor}); err != nil {
		return SentTaskMessage{}, err
	}
	return sent, uc.rec.audit(ctx, "task.message", id.String(), domain.AuditOK, map[string]string{"execution": execution.ID().String()})
}
