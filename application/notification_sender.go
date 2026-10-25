package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// NotificationSender makes one attempt at a delivery: a webhook POST of
// the payload, signed with the target's secret, or a run of the target's
// command with the payload on its stdin. An error is a failed attempt,
// retried later.
type NotificationSender interface {
	Send(ctx context.Context, target domain.NotifyTarget, delivery domain.Delivery) error
}
