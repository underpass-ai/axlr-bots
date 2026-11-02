package application

import (
	"context"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// EnrollNode takes a node's one-time token and signing request and
// hands it a certificate. The token is consumed before the certificate
// is signed, so a failure burns the token rather than issue twice. A
// node already known (one that enrols again, or a revoked one the
// operator readmits with a fresh token) rotates its certificate; a new
// one is created Registered and comes Online when the agent registers.
type EnrollNode struct {
	deps Dependencies
	rec  recorder
}

// NewEnrollNode wires the use case.
func NewEnrollNode(deps Dependencies) EnrollNode {
	return EnrollNode{deps: deps, rec: recorder{deps: deps}}
}

// Execute enrolls the node.
func (uc EnrollNode) Execute(ctx context.Context, request EnrollmentRequest) (EnrollmentResult, error) {
	tokenID, err := domain.NewEnrollmentID(request.TokenID)
	if err != nil {
		return EnrollmentResult{}, err
	}
	enrollment, err := uc.deps.Enrollments.Get(ctx, tokenID)
	if err != nil {
		if domain.IsNotFound(err) {
			return EnrollmentResult{}, domain.ForbiddenError{Reason: "the enrollment token is not known"}
		}
		return EnrollmentResult{}, err
	}
	now := uc.deps.Clock.Now()
	if err := enrollment.Consume(request.Secret, now); err != nil {
		_ = uc.rec.audit(ctx, "node.enroll", enrollment.NodeID().String(), domain.AuditDenied, map[string]string{"token": tokenID.String(), "reason": err.Error()})
		return EnrollmentResult{}, err
	}
	if err := uc.deps.Enrollments.Save(ctx, enrollment); err != nil {
		return EnrollmentResult{}, err
	}
	issued, err := uc.deps.Certifier.IssueNodeCertificate(ctx, enrollment.NodeID(), request.CSRPEM)
	if err != nil {
		_ = uc.rec.audit(ctx, "node.enroll", enrollment.NodeID().String(), domain.AuditFailed, map[string]string{"token": tokenID.String(), "reason": err.Error()})
		return EnrollmentResult{}, err
	}
	ca, err := uc.deps.Certifier.CACertificate(ctx)
	if err != nil {
		return EnrollmentResult{}, err
	}
	node, err := uc.certify(ctx, enrollment, request.HostName, issued, now)
	if err != nil {
		return EnrollmentResult{}, err
	}
	payload := map[string]any{"token": tokenID.String(), "serial": issued.Serial, "notAfter": issued.NotAfter, "hostName": node.HostName(), "labels": node.Labels().Copy()}
	if err := uc.rec.nodeEvent(ctx, domain.EventNodeEnrolled, node.ID(), payload); err != nil {
		return EnrollmentResult{}, err
	}
	details := map[string]string{"token": tokenID.String(), "serial": issued.Serial, "notAfter": issued.NotAfter.UTC().Format(time.RFC3339)}
	if err := uc.rec.audit(ctx, "node.enroll", node.ID().String(), domain.AuditOK, details); err != nil {
		return EnrollmentResult{}, err
	}
	return EnrollmentResult{Node: node, CertificatePEM: issued.CertificatePEM, CAPEM: ca, Serial: issued.Serial, NotAfter: issued.NotAfter}, nil
}

// certify records the certificate on the node, creating it when new. A
// certificate the node held before goes on the denylist: the node's
// record alone would refuse it, but the denylist outlives the record.
func (uc EnrollNode) certify(ctx context.Context, enrollment domain.Enrollment, hostName string, issued IssuedCertificate, now time.Time) (domain.Node, error) {
	if hostName == "" {
		hostName = enrollment.NodeID().String()
	}
	var node domain.Node
	err := uc.deps.Nodes.Update(ctx, enrollment.NodeID(), func(known *domain.Node) error {
		if previous := known.CertificateSerial(); previous != "" && previous != issued.Serial {
			if err := uc.deps.Denylist.Deny(ctx, previous, known.ID().String()); err != nil {
				return err
			}
		}
		if err := uc.pin(known, enrollment, issued, now); err != nil {
			return err
		}
		node = *known
		return nil
	})
	if !domain.IsNotFound(err) {
		return node, err
	}
	// The enrollment's labels are the operator's, set by Certify; the
	// node has reported none yet.
	node, err = domain.NewNode(enrollment.NodeID(), hostName, nil, domain.NodeCapabilities{}, now)
	if err != nil {
		return domain.Node{}, err
	}
	if err := uc.pin(&node, enrollment, issued, now); err != nil {
		return domain.Node{}, err
	}
	if err := uc.deps.Nodes.Save(ctx, node); err != nil {
		return domain.Node{}, err
	}
	return node, nil
}

// pin records the issued certificate, and its expiry, on the node.
func (uc EnrollNode) pin(node *domain.Node, enrollment domain.Enrollment, issued IssuedCertificate, now time.Time) error {
	if err := node.Certify(issued.Serial, enrollment.Labels(), now); err != nil {
		return err
	}
	node.RecordCertificateExpiry(issued.Serial, issued.NotAfter)
	return nil
}
