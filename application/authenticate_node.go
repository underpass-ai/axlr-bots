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

// Execute returns a ForbiddenError when the certificate is refused,
// marked Revoked when the node itself is: then no retry helps, and the
// node says so and stops asking. A serial denied while its node is not
// revoked is one a renewal or a fresh enrollment superseded.
func (uc AuthenticateNode) Execute(ctx context.Context, id domain.NodeID, serial string) error {
	if serial == "" {
		return domain.ForbiddenError{Reason: "a client certificate is required"}
	}
	denied, err := uc.deps.Denylist.IsDenied(ctx, serial)
	if err != nil {
		return err
	}
	node, err := uc.deps.Nodes.Get(ctx, id)
	known := err == nil
	if err != nil && !domain.IsNotFound(err) {
		return err
	}
	if known && node.IsRevoked() {
		return domain.ForbiddenError{Reason: "node " + id.String() + " was revoked: enroll it again with a fresh token", Revoked: true}
	}
	if denied {
		return domain.ForbiddenError{Reason: "the certificate of node " + id.String() + " was revoked"}
	}
	if known && !node.AcceptsCertificate(serial) {
		return domain.ForbiddenError{Reason: "the certificate is not the current one of node " + id.String()}
	}
	return nil
}
