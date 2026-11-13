package application

// ChangeSignal tells when anything the control plane records changes: Next
// answers a channel the next change closes. Take it before reading the
// state, so a change while reading is not missed.
type ChangeSignal interface {
	Next() <-chan struct{}
}
