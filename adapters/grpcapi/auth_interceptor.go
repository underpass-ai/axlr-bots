package grpcapi

import (
	"context"
	"crypto/subtle"
	"github.com/underpass-ai/axlr-bots/application"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// authInterceptor requires `authorization: Bearer <token>` in the
// metadata of every call, as the HTTP operator address requires it in the
// Authorization header. The socket serves without one, as the HTTP socket
// does: whoever reaches it is the service's own user or group.
type authInterceptor struct {
	token string
}

func (a authInterceptor) check(ctx context.Context) error {
	md, _ := metadata.FromIncomingContext(ctx)
	for _, value := range md.Get("authorization") {
		presented, ok := strings.CutPrefix(value, "Bearer ")
		if ok && a.token != "" && subtle.ConstantTimeCompare([]byte(strings.TrimSpace(presented)), []byte(a.token)) == 1 {
			return nil
		}
	}
	return application.UnauthorizedError{}
}

func (a authInterceptor) unary(ctx context.Context, request any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if err := a.check(ctx); err != nil {
		return nil, err
	}
	return handler(ctx, request)
}

func (a authInterceptor) stream(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	if err := a.check(stream.Context()); err != nil {
		return err
	}
	return handler(srv, stream)
}
