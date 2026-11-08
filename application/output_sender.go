package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// OutputSender makes one attempt at an output delivery: a webhook POST
// of the body signed with the target's secret, a POST of the Block Kit
// body to a slack target's incoming webhook, or a run of the target's
// command with the body on its stdin. An error is a failed attempt,
// retried later; it never carries the target's URL or secret.
type OutputSender interface {
	Send(ctx context.Context, target domain.OutputTarget, delivery domain.OutputDelivery) error
}
