package main

import "path/filepath"

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
