package application

import (
	"context"

	"github.com/underpass-ai/underpass-bots/domain"
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
	node, err := uc.deps.Nodes.Get(ctx, id)
	switch {
	case err == nil:
		if err := node.Describe(registration.HostName, labels, registration.Capabilities, now); err != nil {
			return domain.Node{}, err
		}
	case domain.IsNotFound(err):
		node, err = domain.NewNode(id, registration.HostName, labels, registration.Capabilities, now)
		if err != nil {
			return domain.Node{}, err
		}
		node.Heartbeat(now)
	default:
		return domain.Node{}, err
	}
	if err := uc.deps.Nodes.Save(ctx, node); err != nil {
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
