package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// deliverMessage puts the event into the teammate's running task as a
// line of its chat, which the node relays to the run as it relays the
// operator's messages. handled is false when the teammate runs nothing
// on a node: the event is then a task.
func (uc EmitEvent) deliverMessage(ctx context.Context, e emission, mate domain.Bot, sub domain.Subscription) (*EventDelivery, *EventSkip, bool, error) {
	if !mate.AcceptsTasks() {
		delivery, skip, err := skipped(mate, domain.RefusedTargetUnavailable, "bot "+mate.ID().String()+" is "+mate.Status().String()+" and takes no message")
		return delivery, skip, true, err
	}
	task, execution, running, err := runningTaskOf(ctx, uc.deps, mate)
	if err != nil || !running {
		return nil, nil, false, err
	}
	if full, err := intakeFull(ctx, uc.deps, mate); err != nil || full {
		if err != nil {
			return nil, nil, true, err
		}
		delivery, skip, err := uc.deliverNote(ctx, e, mate, sub, DegradedIntakeRate, "")
		return delivery, skip, true, err
	}
	text := "Your teammate " + e.bot.ID().String() + " says (" + e.name.String() + "): " + e.message.String()
	if e.dataJSON != "" {
		text += "\n" + e.dataJSON
	}
	id, err := uc.rec.executionEventID(ctx, domain.EventTaskMessage, mate, task.ID(), execution.ID(), task.CorrelationID(), map[string]any{"text": text, "from": "event:" + e.id})
	if err != nil {
		return nil, nil, true, err
	}
	delivery := &EventDelivery{BotID: mate.ID().String(), Mode: domain.ModeMessage.String(), TaskID: task.ID().String(), MessageID: id.String(), Trigger: sub.TriggerName(), Created: true}
	if err := uc.recordHeard(ctx, e, mate, *delivery); err != nil {
		return nil, nil, true, err
	}
	return delivery, nil, true, nil
}

// runningTaskOf finds the bot's Running task whose execution a node
// holds: the one a message reaches.
func runningTaskOf(ctx context.Context, deps Dependencies, bot domain.Bot) (domain.Task, domain.Execution, bool, error) {
	running, err := deps.Tasks.ListByStatus(ctx, domain.TaskRunning)
	if err != nil {
		return domain.Task{}, domain.Execution{}, false, err
	}
	for _, task := range running {
		if task.BotID() != bot.ID() || task.CurrentExecutionID() == "" {
			continue
		}
		execution, err := deps.Executions.Get(ctx, task.CurrentExecutionID())
		if err != nil {
			return domain.Task{}, domain.Execution{}, false, err
		}
		if !execution.Outcome().IsFinal() && execution.NodeID() != "" {
			return task, execution, true, nil
		}
	}
	return domain.Task{}, domain.Execution{}, false, nil
}
