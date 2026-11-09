package application

import "context"

// ModelGateway reaches a language model with the control plane's key.
type ModelGateway interface {
	Complete(ctx context.Context, call ModelCall) (ModelAnswer, error)
}
