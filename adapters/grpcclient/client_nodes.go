package grpcclient

import (
	"context"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

func (c *Client) ListNodes(ctx context.Context) ([]domain.NodeSnapshot, error) {
	answer, err := c.nodes.ListNodes(ctx, &operatorv1.ListNodesRequest{})
	if err != nil {
		return nil, fromStatus(c.address, err)
	}
	return nodeSnapshots(answer.GetNodes()), nil
}

func (c *Client) GetNode(ctx context.Context, id string) (domain.NodeSnapshot, error) {
	answer, err := c.nodes.GetNode(ctx, &operatorv1.GetNodeRequest{NodeId: id})
	if err != nil {
		return domain.NodeSnapshot{}, fromStatus(c.address, err)
	}
	return nodeSnapshot(answer.GetNode()), nil
}

func (c *Client) CreateEnrollmentToken(ctx context.Context, request unixapi.EnrollTokenRequest) (unixapi.EnrollTokenResponse, error) {
	message, err := enrollmentRequest(request)
	if err != nil {
		return unixapi.EnrollTokenResponse{}, err
	}
	answer, err := c.nodes.CreateEnrollment(ctx, message)
	if err != nil {
		return unixapi.EnrollTokenResponse{}, fromStatus(c.address, err)
	}
	return unixapi.EnrollTokenResponse{Token: answer.GetToken(), ID: answer.GetId(), NodeID: answer.GetNodeId(), Labels: answer.GetLabels(), ExpiresAt: timeOf(answer.GetExpiresAt())}, nil
}

func (c *Client) RevokeNode(ctx context.Context, id string) (unixapi.RevokeResponse, error) {
	answer, err := c.nodes.RevokeNode(ctx, &operatorv1.RevokeNodeRequest{NodeId: id})
	if err != nil {
		return unixapi.RevokeResponse{}, fromStatus(c.address, err)
	}
	return unixapi.RevokeResponse{Node: nodeSnapshot(answer.GetNode()), FailedTasks: nonNil(answer.GetFailedTasks())}, nil
}
