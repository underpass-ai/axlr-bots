package grpcapi

import (
	"context"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
)

// nodesService is NodesService: the operator's side of the fleet.
type nodesService struct {
	operatorv1.UnimplementedNodesServiceServer
	local *unixapi.Local
}

func (s nodesService) ListNodes(ctx context.Context, _ *operatorv1.ListNodesRequest) (*operatorv1.ListNodesResponse, error) {
	nodes, err := s.local.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	return &operatorv1.ListNodesResponse{Nodes: nodesToProto(nodes)}, nil
}

func (s nodesService) GetNode(ctx context.Context, request *operatorv1.GetNodeRequest) (*operatorv1.GetNodeResponse, error) {
	node, err := s.local.GetNode(ctx, request.GetNodeId())
	if err != nil {
		return nil, err
	}
	return &operatorv1.GetNodeResponse{Node: nodeToProto(node)}, nil
}

func (s nodesService) CreateEnrollment(ctx context.Context, request *operatorv1.CreateEnrollmentRequest) (*operatorv1.CreateEnrollmentResponse, error) {
	token, err := s.local.CreateEnrollmentToken(ctx, unixapi.EnrollTokenRequest{NodeID: request.GetNodeId(), Labels: request.GetLabels(), TTL: textOf(request.GetTtl())})
	if err != nil {
		return nil, err
	}
	return &operatorv1.CreateEnrollmentResponse{Token: token.Token, Id: token.ID, NodeId: token.NodeID, Labels: token.Labels, ExpiresAt: timestamp(token.ExpiresAt)}, nil
}

func (s nodesService) RevokeNode(ctx context.Context, request *operatorv1.RevokeNodeRequest) (*operatorv1.RevokeNodeResponse, error) {
	revoked, err := s.local.RevokeNode(ctx, request.GetNodeId())
	if err != nil {
		return nil, err
	}
	return &operatorv1.RevokeNodeResponse{Node: nodeToProto(revoked.Node), FailedTasks: revoked.FailedTasks}, nil
}
