package application

import (
	"context"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// RenewNodeCertificate trades a node's current certificate for a new
// one before it expires, over the connection the current one opened: no
// token is needed, the certificate is the proof. The node's record moves
// to the new serial and the old one goes on the denylist, so the old
// certificate opens nothing from then on. A revoked node, or a
// certificate that is no longer the node's current one, is refused.
type RenewNodeCertificate struct {
	deps Dependencies
	rec  recorder
}

// NewRenewNodeCertificate wires the use case.
func NewRenewNodeCertificate(deps Dependencies) RenewNodeCertificate {
	return RenewNodeCertificate{deps: deps, rec: recorder{deps: deps}}
}

// CertificateRenewal is a node's request for a new certificate: the
// node, the serial of the certificate it presented and its new signing
// request.
type CertificateRenewal struct {
	NodeID domain.NodeID
	Serial string
	CSRPEM []byte
}

// Execute renews the node's certificate.
func (uc RenewNodeCertificate) Execute(ctx context.Context, request CertificateRenewal) (EnrollmentResult, error) {
	id := request.NodeID
	// Checked before signing, so a refused node gets no certificate the
	// control plane would not record; checked again under the node's
	// lock below, where a revoke may have landed meanwhile.
	node, err := uc.deps.Nodes.Get(ctx, id)
	if err != nil {
		if domain.IsNotFound(err) {
			err = domain.ForbiddenError{Reason: "node " + id.String() + " is not known: register before renewing"}
			return EnrollmentResult{}, uc.refused(ctx, id, request.Serial, err)
		}
		return EnrollmentResult{}, err
	}
	if err := admitsRenewal(node, request.Serial); err != nil {
		return EnrollmentResult{}, uc.refused(ctx, id, request.Serial, err)
	}
	issued, err := uc.deps.Certifier.IssueNodeCertificate(ctx, id, request.CSRPEM)
	if err != nil {
		_ = uc.rec.audit(ctx, "node.renew", id.String(), domain.AuditFailed, map[string]string{"serial": request.Serial, "reason": err.Error()})
		return EnrollmentResult{}, err
	}
	ca, err := uc.deps.Certifier.CACertificate(ctx)
	if err != nil {
		return EnrollmentResult{}, err
	}
	now := uc.deps.Clock.Now()
	err = uc.deps.Nodes.Update(ctx, id, func(current *domain.Node) error {
		// Certify would readmit a revoked node: the check comes first.
		if err := admitsRenewal(*current, request.Serial); err != nil {
			return err
		}
		if err := current.Certify(issued.Serial, nil, now); err != nil {
			return err
		}
		current.RecordCertificateExpiry(issued.Serial, issued.NotAfter)
		if err := uc.deps.Denylist.Deny(ctx, request.Serial, id.String()); err != nil {
			return err
		}
		node = *current
		return nil
	})
	if err != nil {
		if domain.IsForbidden(err) {
			return EnrollmentResult{}, uc.refused(ctx, id, request.Serial, err)
		}
		return EnrollmentResult{}, err
	}
	payload := map[string]any{"previousSerial": request.Serial, "serial": issued.Serial, "notAfter": issued.NotAfter}
	if err := uc.rec.nodeEvent(ctx, domain.EventNodeRenewed, id, payload); err != nil {
		return EnrollmentResult{}, err
	}
	details := map[string]string{"previousSerial": request.Serial, "serial": issued.Serial, "notAfter": issued.NotAfter.UTC().Format(time.RFC3339)}
	if err := uc.rec.audit(ctx, "node.renew", id.String(), domain.AuditOK, details); err != nil {
		return EnrollmentResult{}, err
	}
	return EnrollmentResult{Node: node, CertificatePEM: issued.CertificatePEM, CAPEM: ca, Serial: issued.Serial, NotAfter: issued.NotAfter}, nil
}

// admitsRenewal refuses a revoked node and a certificate that is not
// the node's current one: only the certificate the control plane
// recorded may name its successor.
func admitsRenewal(node domain.Node, serial string) error {
	if node.IsRevoked() {
		return domain.ForbiddenError{Reason: "node " + node.ID().String() + " is revoked: enroll it again with a fresh token", Revoked: true}
	}
	if !node.AcceptsCertificate(serial) {
		return domain.ForbiddenError{Reason: "the certificate is not the current one of node " + node.ID().String()}
	}
	return nil
}

// refused audits a denied renewal and answers its error.
func (uc RenewNodeCertificate) refused(ctx context.Context, id domain.NodeID, serial string, err error) error {
	_ = uc.rec.audit(ctx, "node.renew", id.String(), domain.AuditDenied, map[string]string{"serial": serial, "reason": err.Error()})
	return err
}
