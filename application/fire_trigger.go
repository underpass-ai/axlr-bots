package application

import (
	"context"
	"encoding/json"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// FireTrigger creates a task from a webhook delivery or a timer's slot:
// it renders the trigger's objective template over the event and submits
// the task under an idempotency key derived from the trigger and the
// event (vision §14.7), so a redelivered webhook or a scheduler that
// restarts and fires the same slot finds the task it already created.
// Every fire is a bot.trigger.fired event, deduplicated or not.
type FireTrigger struct {
	deps Dependencies
	rec  recorder
}

// NewFireTrigger wires the use case.
func NewFireTrigger(deps Dependencies) FireTrigger {
	return FireTrigger{deps: deps, rec: recorder{deps: deps}}
}

// Execute fires the trigger and returns the task.
func (uc FireTrigger) Execute(ctx context.Context, command FireTriggerCommand) (FireTriggerResult, error) {
	botID, err := domain.NewBotID(command.BotID)
	if err != nil {
		return FireTriggerResult{}, err
	}
	bot, err := uc.deps.Bots.Get(ctx, botID)
	if err != nil {
		return FireTriggerResult{}, err
	}
	trigger, ok := bot.Trigger(command.TriggerName)
	if !ok {
		return FireTriggerResult{}, domain.NotFoundError{Kind: "trigger", ID: botID.String() + "/" + command.TriggerName}
	}
	var (
		event   map[string]any
		input   []byte
		key     domain.IdempotencyKey
		payload = map[string]any{"trigger": trigger.Name(), "type": trigger.Type().String()}
	)
	switch trigger.Type() {
	case domain.TriggerWebhook:
		if err := json.Unmarshal(command.Body, &event); err != nil || event == nil {
			return FireTriggerResult{}, domain.ValidationError{Field: "body", Reason: "must be a JSON object"}
		}
		input = command.Body
		key = domain.FireKeyWebhook(trigger.Name(), command.DeliveryID, command.Body)
		if command.DeliveryID != "" {
			payload["deliveryId"] = command.DeliveryID
		}
	case domain.TriggerTimer:
		if command.ScheduledAt.IsZero() {
			return FireTriggerResult{}, domain.ValidationError{Field: "scheduledAt", Reason: "a timer fire names its slot"}
		}
		scheduled := command.ScheduledAt.UTC().Format(time.RFC3339)
		fired := uc.deps.Clock.Now().UTC().Format(time.RFC3339)
		event = map[string]any{"trigger": trigger.Name(), "scheduledAt": scheduled, "firedAt": fired}
		input, _ = json.Marshal(event)
		key = domain.FireKeyTimer(trigger.Name(), command.ScheduledAt)
		payload["scheduledAt"] = scheduled
	default:
		return FireTriggerResult{}, domain.ValidationError{Field: "trigger", Reason: "a manual trigger is fired by submitting a task"}
	}
	objective, err := domain.RenderObjective(trigger.ObjectiveTemplate(), event)
	if err != nil {
		return FireTriggerResult{}, err
	}
	result, err := submitTask(ctx, uc.deps, submission{action: "trigger.fire", bot: bot, trigger: trigger.Ref(), objective: objective, key: key, input: input})
	if err != nil {
		return FireTriggerResult{}, err
	}
	payload["taskId"] = result.Task.ID().String()
	payload["idempotencyKey"] = key.String()
	payload["created"] = result.Created
	payload["deduplicated"] = !result.Created
	if err := uc.rec.event(ctx, domain.EventTriggerFired, bot, result.Task.ID(), result.Task.CorrelationID(), payload); err != nil {
		return FireTriggerResult{}, err
	}
	return FireTriggerResult{Task: result.Task, Created: result.Created, Trigger: trigger.Ref(), IdempotencyKey: key}, nil
}
