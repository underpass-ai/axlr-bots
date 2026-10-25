package application

import (
	"context"
	"strconv"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// MaxDeliveryAttempts bounds the attempts at one delivery.
const MaxDeliveryAttempts = 6

// deliveryBackoff is the wait after each failed attempt: 10 s, 30 s,
// 1 min, 5 min, 15 min, about 21 minutes in all before the last.
var deliveryBackoff = []time.Duration{10 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute, 15 * time.Minute}

// DeliverNotifications makes the attempts that are due at the queued
// deliveries: one per delivery and pass, the next after a backoff, at
// most MaxDeliveryAttempts. A delivery whose wait ended first (the call
// was decided, expired or the task cancelled) or whose target the bot no
// longer has is skipped: it would announce what is over. Every attempt
// and skip is in the audit log.
type DeliverNotifications struct {
	deps Dependencies
	rec  recorder
}

// NewDeliverNotifications wires the use case.
func NewDeliverNotifications(deps Dependencies) DeliverNotifications {
	return DeliverNotifications{deps: deps, rec: recorder{deps: deps}}
}

// Execute makes the attempts that are due and reports each outcome.
func (uc DeliverNotifications) Execute(ctx context.Context) ([]DeliveryOutcome, error) {
	pending, err := uc.deps.Deliveries.ListPending(ctx)
	if err != nil {
		return nil, err
	}
	var outcomes []DeliveryOutcome
	for _, delivery := range pending {
		if ctx.Err() != nil {
			break
		}
		if !delivery.Due(uc.deps.Clock.Now()) {
			continue
		}
		outcome, err := uc.attempt(ctx, delivery)
		if err != nil {
			return outcomes, err
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes, nil
}

func (uc DeliverNotifications) attempt(ctx context.Context, delivery domain.Delivery) (DeliveryOutcome, error) {
	details := map[string]string{"delivery": delivery.ID().String(), "task": delivery.TaskID().String(), "target": delivery.Target(), "kind": delivery.Kind().String()}
	target, reason, err := uc.target(ctx, delivery)
	if err != nil {
		return DeliveryOutcome{}, err
	}
	now := uc.deps.Clock.Now()
	if reason != "" {
		if err := delivery.Skip(reason, now); err != nil {
			return DeliveryOutcome{}, err
		}
		details["status"], details["reason"] = delivery.Status().String(), reason
		return uc.record(ctx, delivery, domain.AuditOK, details)
	}
	sendErr := uc.deps.Notifier.Send(ctx, target, delivery)
	now = uc.deps.Clock.Now()
	details["attempt"] = strconv.Itoa(delivery.Attempts() + 1)
	switch {
	case sendErr == nil:
		if err := delivery.Delivered(now); err != nil {
			return DeliveryOutcome{}, err
		}
		details["status"] = delivery.Status().String()
		return uc.record(ctx, delivery, domain.AuditOK, details)
	case delivery.Attempts()+1 >= MaxDeliveryAttempts:
		if err := delivery.GiveUp(sendErr.Error(), now); err != nil {
			return DeliveryOutcome{}, err
		}
	default:
		next := now.Add(deliveryBackoff[min(delivery.Attempts(), len(deliveryBackoff)-1)])
		if err := delivery.Retry(sendErr.Error(), next, now); err != nil {
			return DeliveryOutcome{}, err
		}
		details["retryAt"] = next.UTC().Format(time.RFC3339)
	}
	details["status"], details["reason"] = delivery.Status().String(), sendErr.Error()
	return uc.record(ctx, delivery, domain.AuditFailed, details)
}

// target finds where the delivery goes, or says why it is no longer
// worth making.
func (uc DeliverNotifications) target(ctx context.Context, delivery domain.Delivery) (domain.NotifyTarget, string, error) {
	task, err := uc.deps.Tasks.Get(ctx, delivery.TaskID())
	if domain.IsNotFound(err) {
		return domain.NotifyTarget{}, "the task is gone", nil
	}
	if err != nil {
		return domain.NotifyTarget{}, "", err
	}
	wait, ok := task.Approval()
	if task.Status() != domain.TaskWaitingApproval || task.CurrentExecutionID() != delivery.ExecutionID() || !ok || !wait.Open() {
		return domain.NotifyTarget{}, "the wait it announces ended: task " + task.Status().String(), nil
	}
	bot, err := uc.deps.Bots.Get(ctx, delivery.BotID())
	if domain.IsNotFound(err) {
		return domain.NotifyTarget{}, "the bot is gone", nil
	}
	if err != nil {
		return domain.NotifyTarget{}, "", err
	}
	target, ok := bot.Approvals().Target(delivery.Target())
	if !ok || target.Kind() != delivery.Kind() {
		return domain.NotifyTarget{}, "the bot no longer has the target " + delivery.Target(), nil
	}
	return target, "", nil
}

func (uc DeliverNotifications) record(ctx context.Context, delivery domain.Delivery, outcome domain.AuditOutcome, details map[string]string) (DeliveryOutcome, error) {
	if err := uc.deps.Deliveries.Save(ctx, delivery); err != nil {
		return DeliveryOutcome{}, err
	}
	if err := uc.rec.audit(ctx, "approval.notify", delivery.ID().String(), outcome, details); err != nil {
		return DeliveryOutcome{}, err
	}
	return DeliveryOutcome{Delivery: delivery, Reason: details["reason"]}, nil
}
