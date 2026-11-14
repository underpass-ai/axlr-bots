package grpcapi

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

func nodeToProto(s domain.NodeSnapshot) *operatorv1.Node {
	c := s.Capabilities
	return &operatorv1.Node{
		Id: s.ID, HostName: s.HostName, Labels: s.Labels, OperatorLabels: s.OperatorLabels, ReportedLabels: s.ReportedLabels,
		Capabilities: &operatorv1.NodeCapabilities{
			AxlrVersion: c.AXLRVersion, NodeVersion: c.NodeVersion, NodeRevision: c.NodeRevision, PolicySupported: c.PolicySupported,
			CorrelationSupported: c.CorrelationSupported, Dedicated: c.Dedicated, Runtimes: c.Runtimes, Providers: c.Providers, SkillsSupported: c.SkillsSupported,
		},
		Status: s.Status, RegisteredAt: timestamp(s.RegisteredAt), LastSeenAt: timestamp(s.LastSeenAt),
		CertificateSerial: s.CertificateSerial, CertificateNotAfter: timestampOf(s.CertificateNotAfter), RevokedAt: timestampOf(s.RevokedAt),
	}
}

func nodesToProto(snapshots []domain.NodeSnapshot) []*operatorv1.Node {
	out := make([]*operatorv1.Node, 0, len(snapshots))
	for _, s := range snapshots {
		out = append(out, nodeToProto(s))
	}
	return out
}
