package grpcapi

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/underpass-ai/axlr-bots/domain"
)

// actorInterceptor reads the actor the caller asserts in the metadata
// key x-axlr-actor and hands it to the handler through the context. Which
// RPCs use it, and what a decided_by field that differs from it means,
// is the handlers' business (see common.proto). Behind a client
// certificate the actor is the operator the certificate names: with no
// metadata key it is that name, and a key that names anyone else is
// refused, so a certificate cannot decide in another's name.
type actorInterceptor struct{}

func actorFrom(ctx context.Context) (context.Context, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get(ActorMetadata)
	if operator := operatorOf(ctx); operator != "" {
		for _, asserted := range values {
			if asserted != operator {
				return nil, domain.ForbiddenError{Reason: ActorMetadata + " names " + asserted + ", the certificate is for operator " + operator}
			}
		}
		return withActor(ctx, operator), nil
	}
	if len(values) > 0 {
		return withActor(ctx, values[0]), nil
	}
	return ctx, nil
}

func (actorInterceptor) unary(ctx context.Context, request any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	acting, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	return handler(acting, request)
}

func (actorInterceptor) stream(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	acting, err := actorFrom(stream.Context())
	if err != nil {
		return err
	}
	return handler(srv, wrappedStream{ServerStream: stream, ctx: acting})
}
