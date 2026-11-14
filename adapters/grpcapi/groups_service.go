package grpcapi

import (
	"context"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// groupsService is GroupsService over the same service the HTTP routes use.
type groupsService struct {
	operatorv1.UnimplementedGroupsServiceServer
	local *unixapi.Local
}

func (s groupsService) ListGroups(ctx context.Context, _ *operatorv1.ListGroupsRequest) (*operatorv1.ListGroupsResponse, error) {
	groups, err := s.local.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	response := &operatorv1.ListGroupsResponse{}
	for _, g := range groups {
		response.Groups = append(response.Groups, groupToProto(g))
	}
	return response, nil
}

func (s groupsService) GetGroup(ctx context.Context, request *operatorv1.GetGroupRequest) (*operatorv1.GetGroupResponse, error) {
	group, err := s.local.GetGroup(ctx, request.GetName(), request.GetTenant())
	if err != nil {
		return nil, err
	}
	return &operatorv1.GetGroupResponse{Group: groupToProto(group)}, nil
}

func (s groupsService) ListGroupTopics(ctx context.Context, request *operatorv1.ListGroupTopicsRequest) (*operatorv1.ListGroupTopicsResponse, error) {
	topics, err := s.local.ListGroupTopics(ctx, request.GetName(), request.GetTenant())
	if err != nil {
		return nil, err
	}
	return &operatorv1.ListGroupTopicsResponse{Topics: groupTopicsToProto(topics)}, nil
}

func (s groupsService) ListGroupEvents(ctx context.Context, request *operatorv1.ListGroupEventsRequest) (*operatorv1.ListGroupEventsResponse, error) {
	limit, err := limitOf("limit", request.GetLimit())
	if err != nil {
		return nil, err
	}
	events, err := s.local.ListGroupEvents(ctx, application.GroupEventsQuery{Name: request.GetName(), Tenant: request.GetTenant(), After: request.GetAfter(), Limit: limit})
	if err != nil {
		return nil, err
	}
	return &operatorv1.ListGroupEventsResponse{Events: eventsListToProto(events)}, nil
}

func (s groupsService) WatchGroupEvents(request *operatorv1.WatchGroupEventsRequest, stream operatorv1.GroupsService_WatchGroupEventsServer) error {
	ctx := stream.Context()
	read := func(after string) ([]domain.Event, error) {
		return s.local.ListGroupEvents(ctx, application.GroupEventsQuery{Name: request.GetName(), Tenant: request.GetTenant(), After: after, Limit: application.MaxGroupEvents})
	}
	send := func(e domain.Event) error {
		return stream.Send(&operatorv1.WatchGroupEventsResponse{Event: eventToProto(e)})
	}
	return eventWatch{changed: s.local.Changed}.afterCursor(ctx, request.GetAfter(), read, send)
}

func (s groupsService) PublishGroupEvent(ctx context.Context, request *operatorv1.PublishGroupEventRequest) (*operatorv1.PublishGroupEventResponse, error) {
	command := application.PublishGroupEventCommand{
		Group: request.GetName(), Tenant: request.GetTenant(), Name: request.GetEventName(), Message: request.GetMessage(),
		To: request.GetTo(), IdempotencyKey: request.GetIdempotencyKey(),
	}
	if request.GetData() != nil {
		command.Data = request.GetData().AsMap()
	}
	answer, err := s.local.PublishGroupEvent(ctx, command)
	if err != nil {
		return nil, err
	}
	return publishAnswerToProto(answer), nil
}
