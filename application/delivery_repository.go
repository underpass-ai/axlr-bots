package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// DeliveryRepository keeps the notifications of approval requests
// durably: the outbox the daemon delivers from, so a restart resumes the
// retries.
type DeliveryRepository interface {
	Save(ctx context.Context, delivery domain.Delivery) error
	// Get returns a domain.NotFoundError when no delivery has the id.
	Get(ctx context.Context, id domain.DeliveryID) (domain.Delivery, error)
	// ListPending returns every delivery with an attempt to come, in id order.
	ListPending(ctx context.Context) ([]domain.Delivery, error)
}
