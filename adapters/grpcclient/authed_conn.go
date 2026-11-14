package grpcclient

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// maxReceive is how large an answer the client takes: the activity
// snapshot is one message that can pass gRPC's 4 MiB default, and the
// contract has the server raise its send limit for it.
const maxReceive = 64 << 20

// authedConn sends the operator's bearer token in the `authorization`
// metadata key of every call, unary or streaming, and takes large
// answers.
type authedConn struct {
	conn  grpc.ClientConnInterface
	token string
}

func (c authedConn) context(ctx context.Context) context.Context {
	if c.token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.token)
}

func (c authedConn) Invoke(ctx context.Context, method string, args, reply any, opts ...grpc.CallOption) error {
	return c.conn.Invoke(c.context(ctx), method, args, reply, append(opts, grpc.MaxCallRecvMsgSize(maxReceive))...)
}

func (c authedConn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return c.conn.NewStream(c.context(ctx), desc, method, append(opts, grpc.MaxCallRecvMsgSize(maxReceive))...)
}
