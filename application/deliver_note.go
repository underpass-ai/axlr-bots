package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// deliverNote records the event for the teammate's next prompt; no task.
// A Paused teammate's note is recorded too: it reads it when it is
// enabled. A task or message delivery that became a note says why:
// degraded (the receiver's intake) or coalescedInto (its Pending task);
// team, when the teammate took it for its team.
func (uc EmitEvent) deliverNote(ctx context.Context, e emission, mate domain.Bot, sub domain.Subscription, degraded, coalescedInto string, team domain.TeamName) (*EventDelivery, *EventSkip, error) {
	if status := mate.Status(); status != domain.BotEnabled && status != domain.BotPaused {
		return skipped(mate, domain.RefusedTargetUnavailable, "bot "+mate.ID().String()+" is "+status.String()+" and reads no note")
	}
	if coalescedInto != "" {
		if err := carryInto(ctx, uc.deps, domain.TaskID(coalescedInto), e.carried()); err != nil {
			return nil, nil, err
		}
	}
	delivery := &EventDelivery{BotID: mate.ID().String(), Mode: domain.ModeNote.String(), Trigger: sub.TriggerName(), Created: true, Degraded: degraded, CoalescedInto: coalescedInto, Team: team.String()}
	if err := uc.recordHeard(ctx, e, mate, *delivery); err != nil {
		return nil, nil, err
	}
	return delivery, nil, nil
}

// carryInto records the event on the Pending task it was coalesced into,
// when that task expects final events
// (docs/architecture/team-subscriptions.md, "Done is a contract"): the
// task must then emit one for it too. A task that expects none is left
// as it is, so nothing changes for it.
func carryInto(ctx context.Context, deps Dependencies, id domain.TaskID, event domain.CarriedEvent) error {
	task, err := deps.Tasks.Get(ctx, id)
	if err != nil {
		return err
	}
	added, err := task.Carry(event)
	if err != nil || !added {
		return err
	}
	return deps.Tasks.Save(ctx, task)
}

// carried is the event as a task that expects final events carries it.
func (e emission) carried() domain.CarriedEvent {
	return domain.CarriedEvent{EventID: e.id, Name: e.name.String(), ID: domain.DataID(e.data), From: e.from, Message: e.message.String()}
}
