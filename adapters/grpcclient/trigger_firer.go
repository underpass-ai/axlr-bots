package grpcclient

import (
	"context"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
)

// TriggerFirer fires a bot's webhook trigger: the one operation of
// unixapi.Service that is an HTTP route and not an RPC.
type TriggerFirer interface {
	FireTrigger(ctx context.Context, request unixapi.FireRequest) (unixapi.FireResponse, error)
}
