package grpcapi

import (
	"context"

	"google.golang.org/grpc"
)

// wrappedStream is a server stream whose handler sees another context
// (the one an interceptor added the actor to).
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w wrappedStream) Context() context.Context { return w.ctx }
