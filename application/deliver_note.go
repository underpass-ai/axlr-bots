package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// deliverNote records the event for the teammate's next prompt; no task.
// A Paused teammate's note is recorded too: it reads it when it is
// enabled.
func (uc EmitEvent) deliverNote(ctx context.Context, e emission, mate domain.Bot, sub domain.Subscription) (*EventDelivery, *EventSkip, error) {
	if status := mate.Status(); status != domain.BotEnabled && status != domain.BotPaused {
		return skipped(mate, domain.RefusedTargetUnavailable, "bot "+mate.ID().String()+" is "+status.String()+" and reads no note")
	}
	delivery := &EventDelivery{BotID: mate.ID().String(), Mode: domain.ModeNote.String(), Trigger: sub.TriggerName(), Created: true}
	if err := uc.recordHeard(ctx, e, mate, *delivery); err != nil {
		return nil, nil, err
	}
	return delivery, nil, nil
}
