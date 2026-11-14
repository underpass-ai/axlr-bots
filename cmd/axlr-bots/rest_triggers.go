package main

import (
	"context"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
)

// restTriggers fires webhook triggers over the daemon's HTTP socket:
// triggers are HTTP routes, not part of the gRPC operator API. The
// connection is made when a trigger is fired, not before.
type restTriggers struct {
	socket string
}

func (r restTriggers) FireTrigger(ctx context.Context, request unixapi.FireRequest) (unixapi.FireResponse, error) {
	return unixapi.NewClient(r.socket).FireTrigger(ctx, request)
}
