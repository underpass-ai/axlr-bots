package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// RegisterNode takes a node agent's registration: a new node is stored,
// a known one is described again (a restarted agent) and comes Online.
type RegisterNode struct {
	deps Dependencies
	rec  recorder
}

// NewRegisterNode wires the use case.
func NewRegisterNode(deps Dependencies) RegisterNode {
	return RegisterNode{deps: deps, rec: recorder{deps: deps}}
}

// Execute registers the node and returns it.
func (uc RegisterNode) Execute(ctx context.Context, registration NodeRegistration) (domain.Node, error) {
	id, err := domain.NewNodeID(registration.ID)
	if err != nil {
		return domain.Node{}, err
	}
	labels, err := domain.NewNodeLabels(registration.Labels)
	if err != nil {
		return domain.Node{}, err
	}
	now := uc.deps.Clock.Now()
	var node domain.Node
	err = uc.deps.Nodes.Update(ctx, id, func(known *domain.Node) error {
		if err := known.Describe(registration.HostName, labels, registration.Capabilities, now); err != nil {
			return err
		}
		node = *known
		return nil
	})
	switch {
	case domain.IsNotFound(err):
		if node, err = uc.create(ctx, id, labels, registration); err != nil {
			return domain.Node{}, err
		}
	case err != nil:
		return domain.Node{}, err
	}
	payload := map[string]any{"hostName": node.HostName(), "labels": node.Labels().Copy(), "capabilities": node.Capabilities()}
	if err := uc.rec.nodeEvent(ctx, domain.EventNodeRegistered, node.ID(), payload); err != nil {
		return domain.Node{}, err
	}
	if err := uc.rec.audit(ctx, "node.register", node.ID().String(), domain.AuditOK, map[string]string{"host": node.HostName()}); err != nil {
		return domain.Node{}, err
	}
	return node, nil
}

// create stores a node the control plane does not know. Over mutual TLS
// the node's certificate already passed the fleet CA, so the node is
// pinned to it as an enrolled one would be: a control plane that lost
// the node's record takes it back with the certificate it holds, and
// every later call is checked against that serial. The serial is set
// only here; a known node's comes from its enrollment alone.
func (uc RegisterNode) create(ctx context.Context, id domain.NodeID, labels domain.NodeLabels, registration NodeRegistration) (domain.Node, error) {
	now := uc.deps.Clock.Now()
	node, err := domain.NewNode(id, registration.HostName, labels, registration.Capabilities, now)
	if err != nil {
		return domain.Node{}, err
	}
	if registration.CertificateSerial != "" {
		if err := node.Certify(registration.CertificateSerial, nil, now); err != nil {
			return domain.Node{}, err
		}
		node.RecordCertificateExpiry(registration.CertificateSerial, registration.CertificateNotAfter)
	}
	_ = node.Heartbeat(now)
	if err := uc.deps.Nodes.Save(ctx, node); err != nil {
		return domain.Node{}, err
	}
	return node, nil
}
