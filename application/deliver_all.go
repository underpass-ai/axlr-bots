package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// deliverAll gives the event to the teammates, each in its own way, in
// the teammates' order.
func (uc EmitEvent) deliverAll(ctx context.Context, e emission, mates []domain.Bot) ([]EventDelivery, []EventSkip, error) {
	var deliveries []EventDelivery
	var skips []EventSkip
	for _, mate := range mates {
		delivery, skip, err := uc.deliver(ctx, e, mate)
		if err != nil {
			return nil, nil, err
		}
		if delivery != nil {
			deliveries = append(deliveries, *delivery)
		}
		if skip != nil {
			skips = append(skips, *skip)
		}
	}
	return deliveries, skips, nil
}

// eventTaskKey is the key of the task an event makes: one per receiver.
func eventTaskKey(e emission, mate domain.Bot) (domain.IdempotencyKey, error) {
	return domain.NewIdempotencyKey("event:" + e.id + ":" + mate.ID().String())
}
