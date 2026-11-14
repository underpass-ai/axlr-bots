package grpcclient

import (
	"context"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func (c *Client) ListGroups(ctx context.Context) ([]application.Group, error) {
	answer, err := c.groups.ListGroups(ctx, &operatorv1.ListGroupsRequest{})
	if err != nil {
		return nil, fromStatus(c.address, err)
	}
	groups := make([]application.Group, len(answer.GetGroups()))
	for i, group := range answer.GetGroups() {
		groups[i] = groupOf(group)
	}
	return groups, nil
}

func (c *Client) GetGroup(ctx context.Context, name, tenant string) (application.Group, error) {
	answer, err := c.groups.GetGroup(ctx, &operatorv1.GetGroupRequest{Name: name, Tenant: tenant})
	if err != nil {
		return application.Group{}, fromStatus(c.address, err)
	}
	return groupOf(answer.GetGroup()), nil
}

func (c *Client) ListGroupTopics(ctx context.Context, name, tenant string) ([]application.GroupTopic, error) {
	answer, err := c.groups.ListGroupTopics(ctx, &operatorv1.ListGroupTopicsRequest{Name: name, Tenant: tenant})
	if err != nil {
		return nil, fromStatus(c.address, err)
	}
	topics := make([]application.GroupTopic, len(answer.GetTopics()))
	for i, topic := range answer.GetTopics() {
		topics[i] = groupTopicOf(topic)
	}
	return topics, nil
}

func (c *Client) ListGroupEvents(ctx context.Context, query application.GroupEventsQuery) ([]domain.Event, error) {
	answer, err := c.groups.ListGroupEvents(ctx, &operatorv1.ListGroupEventsRequest{Name: query.Name, Tenant: query.Tenant, After: query.After, Limit: int32(query.Limit)})
	if err != nil {
		return nil, fromStatus(c.address, err)
	}
	return eventsOf(answer.GetEvents()), nil
}

func (c *Client) PublishGroupEvent(ctx context.Context, command application.PublishGroupEventCommand) (application.PublishAnswer, error) {
	request, err := publishRequest(command)
	if err != nil {
		return application.PublishAnswer{}, err
	}
	answer, err := c.groups.PublishGroupEvent(ctx, request)
	if err != nil {
		return application.PublishAnswer{}, fromStatus(c.address, err)
	}
	return publishAnswerOf(answer), nil
}
