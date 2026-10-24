package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// AuthenticateNode decides whether a client certificate speaks for a
// node: the serial must not be on the denylist and, when the node is
// known, must be the one recorded at its last enrollment (so a revoked
// node, or a superseded certificate, is refused). A node the control
// plane does not know but whose certificate the CA signed may register:
// that is how a node comes back after the control plane lost its state.
type AuthenticateNode struct {
	deps Dependencies
}

// NewAuthenticateNode wires the use case.
func NewAuthenticateNode(deps Dependencies) AuthenticateNode {
	return AuthenticateNode{deps: deps}
}

// Execute returns a ForbiddenError when the certificate is refused.
func (uc AuthenticateNode) Execute(ctx context.Context, id domain.NodeID, serial string) error {
	if serial == "" {
		return domain.ForbiddenError{Reason: "a client certificate is required"}
	}
	denied, err := uc.deps.Denylist.IsDenied(ctx, serial)
	if err != nil {
		return err
	}
	if denied {
		return domain.ForbiddenError{Reason: "the certificate of node " + id.String() + " was revoked"}
	}
	node, err := uc.deps.Nodes.Get(ctx, id)
	if domain.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !node.AcceptsCertificate(serial) {
		return domain.ForbiddenError{Reason: "the certificate is not the current one of node " + id.String()}
	}
	return nil
}
