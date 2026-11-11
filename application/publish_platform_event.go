package application

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// The control plane's own facts on a team's bus.
const (
	// PlatformTaskFailed: a task failed or expired.
	PlatformTaskFailed domain.EventName = "error.bot"
	// PlatformTaskWaiting: a task waits for a person to approve a call.
	PlatformTaskWaiting domain.EventName = "warn.approval"
	// PlatformTaskDone: a task completed.
	PlatformTaskDone domain.EventName = "done.task"
	// PlatformTaskStuck: the team's Jev watcher judged a running task
	// stuck and the control plane cancelled it.
	PlatformTaskStuck domain.EventName = "warn.stuck"
)

// maxPlatformAnswer bounds the answer done.task carries.
const maxPlatformAnswer = 280

// platformPublisher puts the control plane's own facts about a bot's
// task on the bot's teams' bus, as the bot and by the control plane,
// only when a teammate subscribes to the name: the history grows by what
// the operators asked for. The bot never hears its own. Deliveries follow
// the subscriptions as a bot's event's do, under the task's lineage, so
// a chain of failures is cut by depth and cycle.
type platformPublisher struct {
	deps Dependencies
	emit EmitEvent
}

func newPlatformPublisher(deps Dependencies) platformPublisher {
	return platformPublisher{deps: deps, emit: NewEmitEvent(deps)}
}

// taskEnded publishes done.task for a completed task and error.bot for
// one that failed or expired.
func (p platformPublisher) taskEnded(ctx context.Context, bot domain.Bot, task domain.Task, execution domain.Execution) error {
	switch task.Status() {
	case domain.TaskCompleted:
		answer := []rune(execution.Result().Answer)
		if len(answer) > maxPlatformAnswer {
			answer = answer[:maxPlatformAnswer]
		}
		return p.publish(ctx, bot, task, execution.ID(), PlatformTaskDone, "task "+task.ID().String()+" completed", map[string]any{"taskId": task.ID().String(), "answer": string(answer)})
	case domain.TaskFailed, domain.TaskExpired:
		reason := task.FailureReason()
		return p.publish(ctx, bot, task, execution.ID(), PlatformTaskFailed, "task "+task.ID().String()+" "+strings.ToLower(task.Status().String())+": "+reason, map[string]any{"taskId": task.ID().String(), "reason": reason})
	}
	return nil
}

// taskStuck publishes warn.stuck for a task the team's Jev watcher judged
// stuck: its teammates hear who it was and why, and the one a team
// subscription picks takes its work over.
func (p platformPublisher) taskStuck(ctx context.Context, bot domain.Bot, task domain.Task, execution domain.ExecutionID, reason string) error {
	return p.publish(ctx, bot, task, execution, PlatformTaskStuck, "task "+task.ID().String()+" of "+bot.ID().String()+" was stuck and is cancelled", map[string]any{"taskId": task.ID().String(), "bot": bot.ID().String(), "reason": reason})
}

// taskWaiting publishes warn.approval for a task that waits for a person.
func (p platformPublisher) taskWaiting(ctx context.Context, bot domain.Bot, task domain.Task, execution domain.ExecutionID, tool string) error {
	return p.publish(ctx, bot, task, execution, PlatformTaskWaiting, "task "+task.ID().String()+" waits for a person to approve "+tool, map[string]any{"taskId": task.ID().String(), "tool": tool})
}

func (p platformPublisher) publish(ctx context.Context, bot domain.Bot, task domain.Task, execution domain.ExecutionID, name domain.EventName, text string, data map[string]any) error {
	if bot.Teams().Empty() {
		return nil
	}
	mates, err := teammatesOf(ctx, p.deps, bot)
	if err != nil {
		return err
	}
	listening := false
	for _, mate := range mates {
		if _, hears := mate.Hears(name, bot.ID().String(), domain.PublisherControlPlane, false); hears {
			listening = true
		}
	}
	if !listening {
		return nil
	}
	runes := []rune(strings.Join(strings.Fields(text), " "))
	if len(runes) > domain.MaxEventMessageLength {
		runes = runes[:domain.MaxEventMessageLength]
	}
	message, err := domain.NewEventMessage(string(runes))
	if err != nil {
		return err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	decoded, dataJSON, err := eventData(raw)
	if err != nil {
		return err
	}
	e := emission{execution: execution, by: domain.PublisherControlPlane, from: bot.ID().String(), teams: teamNamesOf(bot), task: task, bot: bot, name: name, severity: domain.KindOf(name).Severity(), message: message, data: decoded, dataJSON: dataJSON}
	e.id = eventID(task.ID(), e)
	recent, err := p.deps.EventReader.ListEvents(ctx, bot.ID(), eventHistory)
	if err != nil {
		return err
	}
	if _, seen, _ := emittedBefore(recent, e.id, p.deps.Clock.Now()); seen {
		return nil
	}
	answer := EmitAnswer{EventID: e.id, Name: e.name.String(), Severity: e.severity}
	if answer.Deliveries, answer.Skipped, err = p.emit.deliverAll(ctx, e, mates); err != nil {
		return err
	}
	return p.emit.recordEmitted(ctx, e, answer)
}
