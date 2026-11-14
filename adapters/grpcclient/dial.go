package grpcclient

import (
	"context"
	"crypto/tls"
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
)

// DialUnix talks to the daemon's gRPC socket (a path, or unix:///path).
// The socket carries no credentials: who can open the file is who may
// operate, as with the HTTP socket.
func DialUnix(socketPath string) (*Client, error) {
	path := socketPath
	if len(path) > 7 && path[:7] == "unix://" {
		path = path[7:]
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	conn, err := grpc.NewClient("unix://"+path, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fromStatus(path, err)
	}
	client := New(conn, path, "")
	client.closer = conn
	return client, nil
}

// DialTLS talks to a control plane's operator address, host:port, over
// TLS with the configuration (the fleet CA as RootCAs) and the bearer
// token on every call.
func DialTLS(address string, config *tls.Config, token string) (*Client, error) {
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(credentials.NewTLS(config)))
	if err != nil {
		return nil, fromStatus(address, err)
	}
	client := New(conn, address, token)
	client.closer = conn
	return client, nil
}

// Reachable reports whether a daemon answers on the gRPC socket.
func Reachable(ctx context.Context, socketPath string) bool {
	client, err := DialUnix(socketPath)
	if err != nil {
		return false
	}
	defer client.Close()
	return client.answers(ctx)
}

// Health is nil when the control plane answers, whatever it answers:
// the operator API has no health call, so the probe is the cheapest
// read, GetBuild, and an answer that refuses (a token it does not know)
// still says the control plane is there.
func (c *Client) Health(ctx context.Context) error {
	if !c.answers(ctx) {
		return fromStatus(c.address, status.Error(codes.Unavailable, "no answer"))
	}
	return nil
}

func (c *Client) answers(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := c.overview.GetBuild(ctx, &operatorv1.GetBuildRequest{})
	if err == nil {
		return true
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled, codes.Unknown:
		return false
	}
	return true
}
