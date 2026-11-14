package grpcclient

import (
	"context"
	"github.com/underpass-ai/axlr-bots/application"

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

func (c *Client) CreateEnrollmentToken(ctx context.Context, request application.EnrollTokenRequest) (application.EnrollTokenResponse, error) {
	message, err := enrollmentRequest(request)
	if err != nil {
		return application.EnrollTokenResponse{}, err
	}
	answer, err := c.nodes.CreateEnrollment(ctx, message)
	if err != nil {
		return application.EnrollTokenResponse{}, fromStatus(c.address, err)
	}
	return application.EnrollTokenResponse{Token: answer.GetToken(), ID: answer.GetId(), NodeID: answer.GetNodeId(), Labels: answer.GetLabels(), ExpiresAt: timeOf(answer.GetExpiresAt())}, nil
}

func (c *Client) RevokeNode(ctx context.Context, id string) (application.RevokeResponse, error) {
	answer, err := c.nodes.RevokeNode(ctx, &operatorv1.RevokeNodeRequest{NodeId: id})
	if err != nil {
		return application.RevokeResponse{}, fromStatus(c.address, err)
	}
	return application.RevokeResponse{Node: nodeSnapshot(answer.GetNode()), FailedTasks: nonNil(answer.GetFailedTasks())}, nil
}
