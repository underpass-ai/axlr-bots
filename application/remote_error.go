package application

// RemoteError is an error the control plane reported to an operator
// client, rebuilt on the client side: it keeps the kind so callers can
// tell a missing task from a refused state, and prints the control
// plane's message.
type RemoteError struct {
	Kind    string
	Message string
}

func (e RemoteError) Error() string { return e.Message }
