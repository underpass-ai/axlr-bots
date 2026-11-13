package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// Transports a command can talk to the control plane with.
const (
	transportGRPC = "grpc"
	transportREST = "rest"
)

// defaultTransport is AXLR_BOTS_TRANSPORT, else gRPC.
func defaultTransport() string {
	if transport := os.Getenv("AXLR_BOTS_TRANSPORT"); transport != "" {
		return transport
	}
	return transportGRPC
}

// validateTransport refuses a word that names neither transport.
func validateTransport(transport string) error {
	if transport != transportGRPC && transport != transportREST {
		return fmt.Errorf("--transport %q: use grpc or rest", transport)
	}
	return nil
}

// grpcSocketPath is where the commands look for the daemon's gRPC
// socket: --grpc-socket or AXLR_BOTS_GRPC_SOCKET, else a file next to
// the state directory's other socket. An explicit empty --socket says
// never to use a daemon, and that holds for gRPC too unless the gRPC
// socket was named.
func grpcSocketPath(configured string, grpcGiven, socketGiven bool, socketPath, stateDir string) string {
	if configured != "" || grpcGiven {
		return configured
	}
	if socketGiven && socketPath == "" {
		return ""
	}
	return filepath.Join(stateDir, grpcSocketFileName)
}
