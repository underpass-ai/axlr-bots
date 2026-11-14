package grpcapi

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
)

// errorInterceptor turns what a handler or an inner interceptor returns
// into the status the contract names (statusError). A handler that
// panics answers INTERNAL instead of taking the daemon down with it.
type errorInterceptor struct{}

func (errorInterceptor) unary(ctx context.Context, request any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (response any, err error) {
	defer recovered(&err)
	response, err = handler(ctx, request)
	return response, statusError(err)
}

func (errorInterceptor) stream(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
	defer recovered(&err)
	return statusError(handler(srv, stream))
}

// recovered turns a panic into the error the caller gets.
func recovered(err *error) {
	if r := recover(); r != nil {
		*err = statusError(fmt.Errorf("internal error: %v", r))
	}
}
