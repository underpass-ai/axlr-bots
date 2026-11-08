package application

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// DeliverOutputs makes the attempts that are due at the queued incident
// outputs, with the approval notifications' rules: one per delivery and
// pass, the next after the same backoff (10 s, 30 s, 1 min, 5 min,
// 15 min), at most MaxDeliveryAttempts. A delivery whose target was
// removed, or replaced by one of another type, is skipped. Every attempt
// and skip is in the audit log as incident.output.
type DeliverOutputs struct {
	deps Dependencies
	rec  recorder
}

// NewDeliverOutputs wires the use case.
func NewDeliverOutputs(deps Dependencies) DeliverOutputs {
	return DeliverOutputs{deps: deps, rec: recorder{deps: deps}}
}

// Execute makes the attempts that are due, oldest first, and reports
// each outcome. Two rules keep a pass short and a channel in order:
//   - an incident's delivery to a target waits while an earlier one of
//     the same incident to the same target is still pending (not due
//     yet, retrying, or held), so a channel never reads "resolved"
//     before "opened";
//   - a target whose attempt failed in this pass gets no other attempt
//     in it: a host that does not answer costs one timeout per pass,
//     not one per delivery.
func (uc DeliverOutputs) Execute(ctx context.Context) ([]OutputDeliveryOutcome, error) {
	pending, err := uc.deps.OutputDeliveries.ListPending(ctx)
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(pending, func(a, b domain.OutputDelivery) int {
		if c := a.CreatedAt().Compare(b.CreatedAt()); c != 0 {
			return c
		}
		return strings.Compare(a.ID().String(), b.ID().String())
	})
	held := map[string]bool{}
	down := map[domain.OutputName]bool{}
	var outcomes []OutputDeliveryOutcome
	for _, delivery := range pending {
		if ctx.Err() != nil {
			break
		}
		line := delivery.IncidentID().String() + "\x00" + delivery.Output().String()
		if held[line] || down[delivery.Output()] || !delivery.Due(uc.deps.Clock.Now()) {
			held[line] = true
			continue
		}
		outcome, err := uc.attempt(ctx, delivery)
		if err != nil {
			return outcomes, err
		}
		outcomes = append(outcomes, outcome)
		if outcome.Delivery.Status() == domain.DeliveryPending {
			held[line], down[delivery.Output()] = true, true
		}
	}
	return outcomes, nil
}

func (uc DeliverOutputs) attempt(ctx context.Context, delivery domain.OutputDelivery) (OutputDeliveryOutcome, error) {
	details := outputDetails(delivery, nil)
	target, err := uc.deps.Outputs.Get(ctx, delivery.Output())
	gone := domain.IsNotFound(err)
	if err != nil && !gone {
		return OutputDeliveryOutcome{}, err
	}
	if gone || target.Kind() != delivery.Kind() {
		reason := "the output " + delivery.Output().String() + " is gone"
		if err := delivery.Skip(reason, uc.deps.Clock.Now()); err != nil {
			return OutputDeliveryOutcome{}, err
		}
		details["status"], details["reason"] = delivery.Status().String(), reason
		return uc.record(ctx, delivery, domain.AuditOK, details)
	}
	sendErr := uc.deps.OutputSender.Send(ctx, target, delivery)
	now := uc.deps.Clock.Now()
	details["attempt"] = strconv.Itoa(delivery.Attempts() + 1)
	switch {
	case sendErr == nil:
		if err := delivery.Delivered(now); err != nil {
			return OutputDeliveryOutcome{}, err
		}
		details["status"] = delivery.Status().String()
		return uc.record(ctx, delivery, domain.AuditOK, details)
	case delivery.Attempts()+1 >= MaxDeliveryAttempts:
		if err := delivery.GiveUp(sendErr.Error(), now); err != nil {
			return OutputDeliveryOutcome{}, err
		}
	default:
		next := now.Add(deliveryBackoff[min(delivery.Attempts(), len(deliveryBackoff)-1)])
		if err := delivery.Retry(sendErr.Error(), next, now); err != nil {
			return OutputDeliveryOutcome{}, err
		}
		details["retryAt"] = next.UTC().Format(time.RFC3339)
	}
	details["status"], details["reason"] = delivery.Status().String(), sendErr.Error()
	return uc.record(ctx, delivery, domain.AuditFailed, details)
}

func (uc DeliverOutputs) record(ctx context.Context, delivery domain.OutputDelivery, outcome domain.AuditOutcome, details map[string]string) (OutputDeliveryOutcome, error) {
	if err := uc.deps.OutputDeliveries.Save(ctx, delivery); err != nil {
		return OutputDeliveryOutcome{}, err
	}
	if err := uc.rec.audit(ctx, "incident.output", delivery.ID().String(), outcome, details); err != nil {
		return OutputDeliveryOutcome{}, err
	}
	return OutputDeliveryOutcome{Delivery: delivery, Reason: details["reason"]}, nil
}
