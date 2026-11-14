package grpcapi

import (
	"google.golang.org/grpc"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
)

// Option changes how the server is built.
type Option func(*Server)

// WithActivity is the ActivityService the server registers; without it
// the service answers UNIMPLEMENTED. This is the one place the activity
// implementation is plugged in.
func WithActivity(activity operatorv1.ActivityServiceServer) Option {
	return func(s *Server) { s.activity = activity }
}

// WithInterceptors puts interceptors outside the server's own (the
// metrics', which must see the code the caller gets, authentication
// failures included).
func WithInterceptors(unary grpc.UnaryServerInterceptor, stream grpc.StreamServerInterceptor) Option {
	return func(s *Server) {
		s.unary = append(s.unary, unary)
		s.stream = append(s.stream, stream)
	}
}

// WithOperatorCertificates makes the TLS operator address take only the
// client certificates the policy names an operator for: without it
// ServeTLS refuses to serve.
func WithOperatorCertificates(operators OperatorCertificates) Option {
	return func(s *Server) { s.operators = operators }
}
