package grpcclient

import (
	"context"
	"github.com/underpass-ai/axlr-bots/application"
)

// TriggerFirer fires a bot's webhook trigger: the one operation of
// application.OperatorService that is an HTTP route and not an RPC.
type TriggerFirer interface {
	FireTrigger(ctx context.Context, request application.FireRequest) (application.FireResponse, error)
}
