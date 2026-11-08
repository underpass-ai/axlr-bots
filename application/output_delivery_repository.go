package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// OutputDeliveryRepository keeps the incident outputs' deliveries
// durably, beside the approval notifications: the outbox the daemon
// delivers from, so a restart resumes the retries.
type OutputDeliveryRepository interface {
	Save(ctx context.Context, delivery domain.OutputDelivery) error
	// Get returns a domain.NotFoundError when no delivery has the id.
	Get(ctx context.Context, id domain.DeliveryID) (domain.OutputDelivery, error)
	// ListPending returns every delivery with an attempt to come, in id order.
	ListPending(ctx context.Context) ([]domain.OutputDelivery, error)
}
