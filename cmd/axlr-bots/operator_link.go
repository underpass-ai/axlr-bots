package main

// operatorLink is everything that says how a command reaches the control
// plane: the transport, the daemon's two sockets and the remote address.
type operatorLink struct {
	// Transport is grpc or rest.
	Transport string
	// Socket is the HTTP socket, used with the rest transport and to
	// fire webhook triggers, which are HTTP routes.
	Socket string
	// GRPCSocket is the gRPC socket; empty is never to use a daemon.
	GRPCSocket string
	Remote     remoteControl
}

func (l operatorLink) grpc() bool {
	return l.Transport != transportREST
}

// daemonSocket is the socket the transport would have dialed.
func (l operatorLink) daemonSocket() string {
	if l.grpc() {
		return l.GRPCSocket
	}
	return l.Socket
}
