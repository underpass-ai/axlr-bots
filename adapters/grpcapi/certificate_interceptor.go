package grpcapi

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"

	"github.com/underpass-ai/axlr-bots/domain"
)

// certificateInterceptor requires the verified client certificate of the
// call to be an operator's and puts the operator's name in the context.
// The TLS handshake already checked the chain against the fleet CA; this
// refuses a certificate of the same CA that is a node's or a service's,
// or was revoked. It runs before the bearer token is looked at.
type certificateInterceptor struct {
	operators OperatorCertificates
}

func (c certificateInterceptor) identify(ctx context.Context) (context.Context, error) {
	info, ok := peer.FromContext(ctx)
	if !ok {
		return nil, domain.ForbiddenError{Reason: "a client certificate is required"}
	}
	tlsInfo, ok := info.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.VerifiedChains) == 0 || len(tlsInfo.State.VerifiedChains[0]) == 0 {
		return nil, domain.ForbiddenError{Reason: "a client certificate is required"}
	}
	name, err := c.operators.Identify(ctx, tlsInfo.State.VerifiedChains[0][0])
	if err != nil {
		return nil, err
	}
	return withOperator(ctx, name), nil
}

func (c certificateInterceptor) unary(ctx context.Context, request any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	identified, err := c.identify(ctx)
	if err != nil {
		return nil, err
	}
	return handler(identified, request)
}

func (c certificateInterceptor) stream(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	identified, err := c.identify(stream.Context())
	if err != nil {
		return err
	}
	return handler(srv, wrappedStream{ServerStream: stream, ctx: identified})
}
