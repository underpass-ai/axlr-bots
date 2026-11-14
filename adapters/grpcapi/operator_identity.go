package grpcapi

import "context"

type operatorKey struct{}

// withOperator keeps the operator the client certificate named.
func withOperator(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, operatorKey{}, name)
}

// operatorOf is the operator the certificate named; empty on the socket,
// which has no certificate.
func operatorOf(ctx context.Context) string {
	name, _ := ctx.Value(operatorKey{}).(string)
	return name
}
