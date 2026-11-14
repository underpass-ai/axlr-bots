package main

// operatorLink is everything that says how a command reaches the control
// plane: the daemon's two sockets and the remote address. The operator
// API is gRPC only.
type operatorLink struct {
	// Socket is the HTTP socket, used to fire webhook triggers, which
	// are HTTP routes, and to tell an older daemon that serves no gRPC.
	Socket string
	// GRPCSocket is the gRPC socket; empty is never to use a daemon.
	GRPCSocket string
	Remote     remoteControl
}
