package grpcclient

import (
	"github.com/underpass-ai/axlr-bots/application"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

func nodeSnapshot(node *operatorv1.Node) domain.NodeSnapshot {
	c := node.GetCapabilities()
	return domain.NodeSnapshot{
		ID: node.GetId(), HostName: node.GetHostName(), Labels: node.GetLabels(), OperatorLabels: node.GetOperatorLabels(), ReportedLabels: node.GetReportedLabels(),
		Capabilities: domain.NodeCapabilities{
			AXLRVersion: c.GetAxlrVersion(), NodeVersion: c.GetNodeVersion(), NodeRevision: c.GetNodeRevision(), PolicySupported: c.GetPolicySupported(),
			CorrelationSupported: c.GetCorrelationSupported(), Dedicated: c.GetDedicated(), Runtimes: c.GetRuntimes(), Providers: c.GetProviders(), SkillsSupported: c.GetSkillsSupported(),
		},
		Status: node.GetStatus(), RegisteredAt: timeOf(node.GetRegisteredAt()), LastSeenAt: timeOf(node.GetLastSeenAt()),
		CertificateSerial: node.GetCertificateSerial(), CertificateNotAfter: timePtr(node.GetCertificateNotAfter()), RevokedAt: timePtr(node.GetRevokedAt()),
	}
}

func nodeSnapshots(nodes []*operatorv1.Node) []domain.NodeSnapshot {
	out := make([]domain.NodeSnapshot, len(nodes))
	for i, node := range nodes {
		out[i] = nodeSnapshot(node)
	}
	return out
}

// enrollmentRequest is the enrollment token request on the wire; the TTL
// is text such as 1h in the command and a Duration there.
func enrollmentRequest(r application.EnrollTokenRequest) (*operatorv1.CreateEnrollmentRequest, error) {
	message := &operatorv1.CreateEnrollmentRequest{NodeId: r.NodeID, Labels: r.Labels}
	if r.TTL != "" {
		ttl, err := time.ParseDuration(r.TTL)
		if err != nil {
			return nil, domain.ValidationError{Field: "ttl", Reason: "must be a duration such as 1h"}
		}
		message.Ttl = durationpb.New(ttl)
	}
	return message, nil
}
