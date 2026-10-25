package application

import (
	"context"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// RecordNodeCertificate keeps the expiry of the certificate a node
// presented over mutual TLS, so the operator sees when it runs out. A
// node enrolled before the control plane kept expiries gets one on its
// next call.
type RecordNodeCertificate struct {
	deps Dependencies
}

// NewRecordNodeCertificate wires the use case.
func NewRecordNodeCertificate(deps Dependencies) RecordNodeCertificate {
	return RecordNodeCertificate{deps: deps}
}

// Execute records the expiry when the serial is the node's current
// certificate and the expiry is new; the node is saved only then. An
// unknown node records nothing.
func (uc RecordNodeCertificate) Execute(ctx context.Context, id domain.NodeID, serial string, notAfter time.Time) error {
	node, err := uc.deps.Nodes.Get(ctx, id)
	if domain.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !node.RecordCertificateExpiry(serial, notAfter) {
		return nil
	}
	return uc.deps.Nodes.Save(ctx, node)
}
