package application

import (
	"errors"

	"github.com/underpass-ai/axlr-bots/domain"
)

// NodePush is what the control plane pushes to a node on its stream: an
// execution the node runs that it should renew now (it was cancelled,
// or a message reached it), or All: renew every execution it runs (the
// stream opened, or the node fell behind). Ack, when the adapter sets
// it, tells the bus the push was taken; nil needs no answer.
type NodePush struct {
	Execution domain.ExecutionID
	All       bool
	Ack       func() `json:"-"`
}

// ErrNoStream is a control plane that serves no node stream (an older
// one): the node renews its leases on its own clock only.
var ErrNoStream = errors.New("the control plane serves no node stream")
