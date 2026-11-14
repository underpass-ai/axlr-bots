package grpcapi

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// actorInterceptor reads the actor the caller asserts in the metadata
// key x-axlr-actor and hands it to the handler through the context. Which
// RPCs use it, and what a decided_by field that differs from it means,
// is the handlers' business (see common.proto).
type actorInterceptor struct{}

func actorFrom(ctx context.Context) context.Context {
	md, _ := metadata.FromIncomingContext(ctx)
	if values := md.Get(ActorMetadata); len(values) > 0 {
		return withActor(ctx, values[0])
	}
	return ctx
}

func (actorInterceptor) unary(ctx context.Context, request any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	return handler(actorFrom(ctx), request)
}

func (actorInterceptor) stream(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	return handler(srv, wrappedStream{ServerStream: stream, ctx: actorFrom(stream.Context())})
}
