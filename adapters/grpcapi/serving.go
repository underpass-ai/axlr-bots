package grpcapi

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// shutdownGrace is how long streams get to end when the server stops
// before they are cut: a watch never ends on its own.
const shutdownGrace = 5 * time.Second

// ServeTLS serves the API on the listener over TLS behind the bearer
// token until ctx ends: the configuration is the fleet CA's
// (pki.ServerTLSConfig), the token the operator's.
func (s *Server) ServeTLS(ctx context.Context, listener net.Listener, config *tls.Config, token string) error {
	if token == "" {
		return errors.New("grpcapi: the operator address needs a bearer token")
	}
	return s.serve(ctx, listener, s.build(token, grpc.Creds(credentials.NewTLS(config))), func() {})
}

// ServeSocket serves the API on a Unix socket until ctx ends, with no
// token: the file is 0660 so the service's user and group reach it and
// nobody else, as for the HTTP socket. A socket file left by a dead
// daemon is removed first.
func (s *Server) ServeSocket(ctx context.Context, socketPath string) error {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return fmt.Errorf("grpcapi: %w", err)
	}
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("grpcapi: stale socket: %w", err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("grpcapi: %w", err)
	}
	if err := os.Chmod(socketPath, 0o660); err != nil {
		_ = listener.Close()
		return fmt.Errorf("grpcapi: %w", err)
	}
	return s.serve(ctx, listener, s.build(""), func() { _ = os.Remove(socketPath) })
}

// serve runs the gRPC server on the listener until ctx ends, then stops
// it gracefully and, after the grace, for good.
func (s *Server) serve(ctx context.Context, listener net.Listener, server *grpc.Server, cleanup func()) error {
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
		stopped := make(chan struct{})
		go func() { server.GracefulStop(); close(stopped) }()
		select {
		case <-stopped:
		case <-time.After(shutdownGrace):
			server.Stop()
		}
		<-done
		cleanup()
		return nil
	case err := <-done:
		cleanup()
		if err != nil {
			return fmt.Errorf("grpcapi: %w", err)
		}
		return nil
	}
}
