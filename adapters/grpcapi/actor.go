package grpcapi

import "context"

// ActorMetadata is the metadata key that names who decides on the
// RPCs that decide: the gRPC side of the HTTP API's X-Axlr-Actor header.
const ActorMetadata = "x-axlr-actor"

type actorKey struct{}

// withActor keeps the actor the caller asserted in the context.
func withActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

// actorOf is the actor the caller asserted; empty is the control plane's own.
func actorOf(ctx context.Context) string {
	actor, _ := ctx.Value(actorKey{}).(string)
	return actor
}
