package application

import (
	"context"
	"encoding/json"

	"github.com/underpass-ai/axlr-bots/domain"
)

// intakeWindow is the span a receiver's intake is counted over.
const intakeWindow = eventRateWindow

// Reasons a task or message delivery became a note.
const (
	// DegradedIntakeRate: the receiver took its most events this minute.
	DegradedIntakeRate = "intake_rate"
)

// eventChainDepthLimit is the deepest a chain an event of the task may
// go: the least of the emitter's events.maxDepth and its chain's limit.
func eventChainDepthLimit(bot domain.Bot, task domain.Task) int {
	limit := bot.Events().MaxDepth()
	if lineage, ok := task.Lineage(); ok && lineage.MaxDepth < limit {
		limit = lineage.MaxDepth
	}
	return limit
}

// intakeFull reports whether the receiver took its most events as tasks
// or messages in the last minute; notes do not count.
func intakeFull(ctx context.Context, deps Dependencies, mate domain.Bot) (bool, error) {
	events, err := deps.EventReader.ListEvents(ctx, mate.ID(), eventHistory)
	if err != nil {
		return false, err
	}
	now, count := deps.Clock.Now(), 0
	for _, event := range events {
		if event.Type != domain.EventEventHeard || now.Sub(event.CreatedAt) >= intakeWindow {
			continue
		}
		if mode := event.Payload["mode"]; mode == domain.ModeTask.String() || mode == domain.ModeMessage.String() {
			count++
		}
	}
	return count >= mate.Events().IntakePerMinute(), nil
}

// pendingTaskOf finds the receiver's Pending task, not started yet, that
// an event of the name made: the task a later event of the name is
// coalesced into.
func pendingTaskOf(ctx context.Context, deps Dependencies, mate domain.Bot, name domain.EventName) (domain.Task, bool, error) {
	pending, err := deps.Tasks.ListByStatus(ctx, domain.TaskPending)
	if err != nil {
		return domain.Task{}, false, err
	}
	for _, task := range pending {
		if task.BotID() != mate.ID() || task.Trigger().Type != domain.TriggerEvent || task.CurrentExecutionID() != "" {
			continue
		}
		var input struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(task.Input(), &input) == nil && input.Name == name.String() {
			return task, true, nil
		}
	}
	return domain.Task{}, false, nil
}
